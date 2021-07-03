package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"sync"
	"time"
)

// based on https://github.com/pdf/kodirpc

const (
	// backoff base value
	backoff = 10 * time.Millisecond
	// DefaultReadTimeout is the default time a call will wait for a response.
	DefaultReadTimeout = 5 * time.Second
	// DefaultConnectTimeout is the default time re-/connection will be attempted before failure.
	DefaultConnectTimeout = 5 * time.Minute
	// DefaultReconnect determines whether the client reconnects by default.
	DefaultReconnect = true
	// DefaultConnectBackoffScale is the default back-off scaling factor
	DefaultConnectBackoffScale = 2
)

// Config represents the user-configurable parameters for the client
type Config struct {
	// ReadTimeout is the time a call will wait for a response before failure.
	ReadTimeout time.Duration
	// ConnectTimeout is the time a re-/connection will be attempted before failure.
	// A value of zero attempts indefinitely.
	ConnectTimeout time.Duration
	// Reconnect determines whether the client will attempt to reconnect on connection failure
	Reconnect bool
	// ConnectBackoffScale sets the scaling factor for back-off on failed connection attempts
	ConnectBackoffScale int
}

// NewConfig returns a config instance with default values.
func NewConfig() (c *Config) {
	return &Config{
		ReadTimeout:         DefaultReadTimeout,
		ConnectTimeout:      DefaultConnectTimeout,
		Reconnect:           DefaultReconnect,
		ConnectBackoffScale: DefaultConnectBackoffScale,
	}
}

// request is used internally to encode outbound requests
type request struct {
	Version string      `json:"jsonrpc"`
	Method  *string     `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
	ID      *uint64     `json:"id,omitempty"`
}

// response is used internally to decode replies
type response struct {
	Result interface{} `json:"result,omitempty"`
	Error  *Error      `json:"error,omitempty"`
	request
}

// Client is a TCP JSON-RPC client for Kodi.
type Client struct {
	address string
	config  *Config
	conn    io.ReadWriteCloser
	enc     *json.Encoder
	dec     *json.Decoder

	pending  map[uint64]chan response
	handlers map[string][]NotificationHandler
	seq      uint64

	quitChan chan struct{}

	closed bool
	sync.RWMutex
}

// Close the client connection, not further use of the Client is permitted after
// this method has been called.
func (c *Client) Close() error {
	c.Lock()
	if c.closed {
		return fmt.Errorf(`Double close`)
	}
	c.closed = true
	close(c.quitChan)
	for id := range c.pending {
		close(c.pending[id])
		delete(c.pending, id)
	}
	for method := range c.handlers {
		delete(c.handlers, method)
	}
	err := c.conn.Close()
	c.Unlock()
	return err
}

// Handle the notification method, using the specificed handler.  The handler
// will be passed the data parameter from the incoming notification.
func (c *Client) Handle(method string, handler NotificationHandler) {
	c.Lock()
	if _, ok := c.handlers[method]; !ok {
		c.handlers[method] = make([]NotificationHandler, 0, 1)
	}
	c.handlers[method] = append(c.handlers[method], handler)
	c.Unlock()
}

// Notify sends the RPC request and does not wait for a response.
func (c *Client) Notify(method string, params interface{}) error {
	_, _, err := c.call(method, params, false)
	return err
}

// Call an RPC method and return the result.
func (c *Client) Call(method string, params interface{}) (interface{}, error) {
	var res response
	id, ch, err := c.call(method, params, true)
	if err != nil {
		return nil, err
	}
	select {
	case res = <-ch:
		c.clearPending(id)
	case <-time.After(c.config.ReadTimeout):
		c.clearPending(id)
		return nil, fmt.Errorf(`Timed out`)
	case <-c.quitChan:
		return nil, fmt.Errorf(`Closing`)
	}

	if res.Error != nil {
		err = res.Error
	}
	return res.Result, err
}

// clearPending removes a request from the pending response list
func (c *Client) clearPending(id uint64) {
	c.Lock()
	close(c.pending[id])
	delete(c.pending, id)
	c.Unlock()
}

// call encodes and sends the specified method and params, optionally setting up a pending response handler
func (c *Client) call(method string, params interface{}, withResponse bool) (uint64, chan response, error) {
	var (
		ch  chan response
		id  uint64
		req = request{
			Version: `2.0`,
			Method:  &method,
			Params:  params,
		}
	)
	if withResponse {
		ch = make(chan response)
		c.Lock()
		id = c.seq
		c.seq++
		c.pending[id] = ch
		c.Unlock()
		req.ID = &id
	}
	// Block during reconnect
	c.RLock()
	err := c.enc.Encode(req)
	c.RUnlock()
	return id, ch, err
}

// reader reads messages from the socket and delivers them for processing
func (c *Client) reader() {
	var (
		res response
		err error
	)
	for {
		select {
		case <-c.quitChan:
			return
		default:
			res = response{}
			// Block during reconnect
			c.RLock()
			c.RUnlock()
			err = c.dec.Decode(&res)
			if err != nil {
				// Exit the loop if we closed the connection
				c.RLock()
				if c.closed {
					c.RUnlock()
					return
				}
				c.RUnlock()
				if _, ok := err.(net.Error); ok || err == io.EOF {
					logger.Warnf("Disconnected: %v", err)
					// On a network error, initiate reconnect logic if enabled,
					// otherwise close this client
					if c.config.Reconnect {
						if err = c.dial(); err != nil {
							if err = c.Close(); err != nil {
								logger.Errorf("Failed to clean up: %v", err)
							}
							return
						}
					} else {
						if err = c.Close(); err != nil {
							logger.Errorf("Failed to clean up: %v", err)
						}
						return
					}
				} else {
					// Unknown error, probably failed decoding for some reason
					logger.Warnf("Failed decoding message: %v", err)
				}
				continue
			}
			c.process(res)
		}
	}
}

// process routes messages to the appropriate handlers or pending responses
func (c *Client) process(res response) {
	if res.ID != nil {
		c.RLock()
		// Since we have a response ID, we should have a corresponding pending
		// response
		ch, ok := c.pending[*res.ID]
		if !ok {
			c.RUnlock()
			logger.Warnf("Unknown request ID: %d", *res.ID)
			return
		}
		// Stay locked around the channel write so that we don't write to the
		// chan after close
		ch <- res
		c.RUnlock()
		return
	}
	if res.Method != nil {
		// Should be a notification since we did not get a response ID
		params, ok := res.Params.(map[string]interface{})
		if !ok {
			logger.Warnf("Received notification with malformed params: %+v", res.Params)
			return
		}
		c.RLock()
		if _, ok := c.handlers[*res.Method]; !ok {
			c.RUnlock()
			logger.Debugf("Unclaimed notification (%s): %+v", *res.Method, res)
			return
		}
		// Copy the handlers here so that we can release the lock before
		// processing
		handlers := make([]NotificationHandler, len(c.handlers[*res.Method]))
		copy(handlers, c.handlers[*res.Method])
		c.RUnlock()

		for _, handler := range handlers {
			go handler(*res.Method, params["data"])
		}
		return
	}

	// Should not reach here
	logger.Warnf("Unhandled message: %+v", res)
	return
}

// dial initiates a connection, and retries with back-off if enabled
func (c *Client) dial() (err error) {
	var (
		attempt  float64 = 1
		duration time.Duration
	)
	c.Lock()
	defer c.Unlock()

	logger.Infof("Connecting (%s)", c.address)
	for {
		c.conn, err = net.Dial(`tcp`, c.address)
		if err != nil {
			duration = time.Duration(math.Pow(attempt, float64(c.config.ConnectBackoffScale))) * backoff
			if duration < 0 {
				// wrapped, so trip our timeout
				duration = c.config.ConnectTimeout + 1
				if duration < 0 {
					return fmt.Errorf("ConnectTimeout is set to max duration value, can never be exceeded!")
				}
			}
			if !c.config.Reconnect || (c.config.ConnectTimeout != 0 && duration > c.config.ConnectTimeout) {
				return fmt.Errorf("Could not establish connection (%s): %v", c.address, err)
			}
			logger.Debugf("Sleeping for %dms/%dms", duration/time.Millisecond, c.config.ConnectTimeout/time.Millisecond)
			time.Sleep(duration)
			attempt++
			continue
		}
		c.enc = json.NewEncoder(c.conn)
		c.dec = json.NewDecoder(c.conn)
		logger.Infof("Connected (%s)", c.address)
		return nil
	}
}

// NewClient connects to the specified address and returns the resulting Client.
func NewClient(address string, config *Config) (c *Client, err error) {
	if config == nil {
		config = NewConfig()
	}
	c = &Client{
		address:  address,
		config:   config,
		pending:  make(map[uint64]chan response),
		handlers: make(map[string][]NotificationHandler),
		quitChan: make(chan struct{}),
	}
	if err = c.dial(); err != nil {
		return nil, err
	}
	go c.reader()

	return c, nil
}

// LevelledLogger represents a minimal levelled logger
type LevelledLogger interface {
	// Debugf handles debug level messages
	Debugf(format string, args ...interface{})
	// Infof handles info level messages
	Infof(format string, args ...interface{})
	// Warnf handles warn level messages
	Warnf(format string, args ...interface{})
	// Errorf handles error level messages
	Errorf(format string, args ...interface{})
	// Fatalf handles fatal level messages, and must exit the application
	Fatalf(format string, args ...interface{})
	// Panicf handles debug level messages, and must panic the application
	Panicf(format string, args ...interface{})
}

// stubLogger satisfies the Logger interface, and simply does nothing with
// received messages
type stubLogger struct{}

// Debugf handles debug level messages
func (l *stubLogger) Debugf(format string, args ...interface{}) {}

// Infof handles info level messages
func (l *stubLogger) Infof(format string, args ...interface{}) {}

// Warnf handles warn level messages
func (l *stubLogger) Warnf(format string, args ...interface{}) {}

// Errorf handles error level messages
func (l *stubLogger) Errorf(format string, args ...interface{}) {}

// Fatalf handles fatal level messages, exits the application
func (l *stubLogger) Fatalf(format string, args ...interface{}) {
	os.Exit(1)
}

// Panicf handles debug level messages, and panics the application
func (l *stubLogger) Panicf(format string, args ...interface{}) {
	panic(fmt.Sprintf(format, args...))
}

type logPrefixer struct {
	log LevelledLogger
	sync.Mutex
}

// Debugf handles debug level messages, prefixing them for kodirpc
func (l *logPrefixer) Debugf(format string, args ...interface{}) {
	l.Lock()
	l.log.Debugf(l.prefix(format), args...)
	l.Unlock()
}

// Infof handles info level messages, prefixing them for kodirpc
func (l *logPrefixer) Infof(format string, args ...interface{}) {
	l.Lock()
	l.log.Infof(l.prefix(format), args...)
	l.Unlock()
}

// Warnf handles warn level messages, prefixing them for kodirpc
func (l *logPrefixer) Warnf(format string, args ...interface{}) {
	l.Lock()
	l.log.Warnf(l.prefix(format), args...)
	l.Unlock()
}

// Errorf handles error level messages, prefixing them for kodirpc
func (l *logPrefixer) Errorf(format string, args ...interface{}) {
	l.Lock()
	l.log.Errorf(l.prefix(format), args...)
	l.Unlock()
}

// Fatalf handles fatal level messages, prefixing them for kodirpc
func (l *logPrefixer) Fatalf(format string, args ...interface{}) {
	l.Lock()
	l.log.Fatalf(l.prefix(format), args...)
	l.Unlock()
}

// Panicf handles debug level messages, prefixing them for kodirpc
func (l *logPrefixer) Panicf(format string, args ...interface{}) {
	l.Lock()
	l.log.Panicf(l.prefix(format), args...)
	l.Unlock()
}

func (l *logPrefixer) prefix(format string) string {
	return `[kodirpc] ` + format
}

var (
	// logger holds the global logger used by kodirpc, can be set via
	// SetLogger() in the kodirpc package
	logger LevelledLogger
)

func init() {
	SetLogger(&stubLogger{})
}

// SetLogger enables logging for the library and wraps the supplied logger with
// a logPrefixer to denote locally generated logs
func SetLogger(l LevelledLogger) {
	logger = &logPrefixer{log: l}
}

// NotificationHandler is a callback handler for notifications.
type NotificationHandler func(method string, data interface{})

// Error response.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error satisfies the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s [code: %d]", e.Message, e.Code)
}
