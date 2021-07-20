package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/h2non/filetype"
	log "github.com/sirupsen/logrus"

	"github.com/buger/jsonparser"
	"github.com/jerblack/base"
	"github.com/jerblack/server_tools/castctl/cast"
	pb "github.com/jerblack/server_tools/castctl/cast/proto"
)

var (
	// Global request id
	requestID int
)

const (
	// 'CC1AD845' seems to be a predefined app; check link
	// https://gist.github.com/jloutsenhizer/8855258
	defaultChromecastAppID = "CC1AD845"

	defaultSender = "sender-0"
	defaultRecv   = "receiver-0"

	namespaceConn  = "urn:x-cast:com.google.cast.tp.connection"
	namespaceRecv  = "urn:x-cast:com.google.cast.receiver"
	namespaceMedia = "urn:x-cast:com.google.cast.media"
)

type PlayedItem struct {
	ContentID string `json:"content_id"`
	Started   int64  `json:"started"`
	Finished  int64  `json:"finished"`
}

type CastMessageFunc func(*pb.CastMessage)

type Application struct {
	conn  *cast.Connection
	debug bool

	// 'cast.Connection' will send receieved messages back on this channel.
	recvMsgChan chan *pb.CastMessage
	// Internal mapping of request id to result channel
	resultChanMap map[int]chan *pb.CastMessage

	messageMu sync.Mutex
	// Relay messages receieved so users can add custom logic to
	// events.
	messageChan chan *pb.CastMessage
	// Functions that will receieve messages from 'messageChan'
	messageFuncs []CastMessageFunc

	// Current values from the chromecast.
	application *cast.Application // It is possible that there is no current application, can happen for google home.
	media       *cast.Media
	// There seems to be two different volumes returned from the chromecast,
	// one for the receiever and one for the playing media. It looks we update
	// the receiever volume from go-chromecast so we should use that one. But
	// we will keep the other one around in-case we need it at some point.
	volumeMedia    *cast.Volume
	volumeReceiver *cast.Volume

	httpServer *http.ServeMux
	serverPort int
	localIP    string
	iface      *net.Interface

	// NOTE: Currently only playing one media file at a time is handled
	mediaFinished  chan bool
	mediaFilenames []string

	// Number of connection retries to try before returning
	// and error.
	connectionRetries int
	mapLock           *sync.RWMutex
}

type ApplicationOption func(*Application)

func WithIface(iface *net.Interface) ApplicationOption {
	return func(a *Application) {
		a.iface = iface
	}
}

func WithServerPort(port int) ApplicationOption {
	return func(a *Application) {
		a.serverPort = port
	}
}

func WithDebug(debug bool) ApplicationOption {
	return func(a *Application) {
		a.debug = debug
		a.conn.SetDebug(debug)
	}
}

func WithConnectionRetries(connectionRetries int) ApplicationOption {
	return func(a *Application) {
		a.connectionRetries = connectionRetries
	}
}

func NewApplication(opts ...ApplicationOption) *Application {
	lock := sync.RWMutex{}
	recvMsgChan := make(chan *pb.CastMessage, 5)
	a := &Application{
		recvMsgChan:       recvMsgChan,
		resultChanMap:     map[int]chan *pb.CastMessage{},
		messageChan:       make(chan *pb.CastMessage),
		conn:              cast.NewConnection(recvMsgChan),
		connectionRetries: 5,
		mapLock:           &lock,
	}

	// Apply options
	for _, o := range opts {
		o(a)
	}

	// Kick off the listener for asynchronous messages received from the
	// cast connection.
	go a.recvMessages()
	// Kick off the message channel listener.
	go a.messageChanHandler()
	return a
}

func (a *Application) Application() *cast.Application { return a.application }
func (a *Application) Media() *cast.Media             { return a.media }
func (a *Application) Volume() *cast.Volume           { return a.volumeReceiver }

func (a *Application) AddMessageFunc(f CastMessageFunc) {
	a.messageMu.Lock()
	defer a.messageMu.Unlock()

	a.messageFuncs = append(a.messageFuncs, f)
}

func (a *Application) messageChanHandler() {
	for msg := range a.messageChan {
		a.messageMu.Lock()
		for _, f := range a.messageFuncs {
			f(msg)
		}
		a.messageMu.Unlock()
	}
}

func (a *Application) MediaStart() {
	a.mediaFinished = make(chan bool, 1)
}

func (a *Application) MediaWait() {
	<-a.mediaFinished
	a.mediaFinished = nil
}

func (a *Application) MediaFinished() {
	if a.mediaFinished == nil {
		return
	}
	a.mediaFinished <- true
}

func (a *Application) recvMessages() {
	for msg := range a.recvMsgChan {
		requestID, err := jsonparser.GetInt([]byte(*msg.PayloadUtf8), "requestId")
		if err == nil {
			if resultChan, ok := a.resultChanMap[int(requestID)]; ok {
				resultChan <- msg
				// Relay the event to any user specified message funcs.
				a.messageChan <- msg
				continue
			}
		}

		messageBytes := []byte(*msg.PayloadUtf8)
		// This already gets checked in the cast.Connection.handleMessage function.
		messageType, _ := jsonparser.GetString(messageBytes, "type")
		switch messageType {
		case "LOAD_FAILED":
			a.MediaFinished()
		case "MEDIA_STATUS":
			resp := cast.MediaStatusResponse{}
			if err = json.Unmarshal(messageBytes, &resp); err == nil {
				for _, status := range resp.Status {
					// The LoadingItemId is only set when there is a playlist and there
					// is an item being loaded to play next.
					if status.IdleReason == "FINISHED" && status.LoadingItemId == 0 {
						a.MediaFinished()
					} else if status.IdleReason == "INTERRUPTED" && status.Media.ContentId == "" {
						// This can happen when we go "next" in a playlist when it
						// is playing the last track.
						a.MediaFinished()
					}
				}
			}
		case "RECEIVER_STATUS":
			// We don't care about this when the application isn't set.
			if a.application == nil {
				break
			}
			resp := cast.ReceiverStatusResponse{}
			if err = json.Unmarshal(messageBytes, &resp); err == nil {
				// Check to see if the application on the device has changed,
				// if it has it is likely not this running instance that changed
				// it because that currently isn't possible.
				for _, app := range resp.Status.Applications {
					if app.AppId != a.application.AppId {
						a.MediaFinished()
					}
					a.application = &app
				}
				a.volumeReceiver = &resp.Status.Volume
			}
		}
		// Relay the event to any user specified message funcs.
		a.messageChan <- msg
	}
}

func (a *Application) SetDebug(debug bool) { a.debug = debug; a.conn.SetDebug(debug) }

func (a *Application) Start(addr string, port int) error {
	if err := a.conn.Start(addr, port); err != nil {
		return err
	}
	if err := a.sendDefaultConn(&cast.ConnectHeader); err != nil {
		return errMsg(err, "unable to connect to chromecast")
	}
	return a.Update()
}

func (a *Application) Update() error {
	var recvStatus *cast.ReceiverStatusResponse
	var err error
	// Simple retry. We need this for when the device isn't currently
	// available, but it is likely that it will come up soon. If the device
	// has switch network addresses the caller is expected to handle that situation.
	for i := 0; i < a.connectionRetries; i++ {
		recvStatus, err = a.getReceiverStatus()
		if err == nil {
			break
		}
		a.log("error getting receiever status: %v", err)
		a.log("unable to get status from device; attempt %d/5, retrying...", i+1)
		time.Sleep(time.Second * 2)
	}
	if err != nil {
		return err
	}

	if len(recvStatus.Status.Applications) > 1 {
		a.log("more than 1 connected application on the chromecast: (%d)%#v", len(recvStatus.Status.Applications), recvStatus.Status.Applications)
	}

	for _, app := range recvStatus.Status.Applications {
		a.application = &app
	}
	a.volumeReceiver = &recvStatus.Status.Volume

	if a.application == nil || a.application.IsIdleScreen {
		return nil
	}

	a.updateMediaStatus()

	return nil

}

func (a *Application) updateMediaStatus() error {
	a.sendMediaConn(&cast.ConnectHeader)

	mediaStatus, err := a.getMediaStatus()
	if err != nil {
		return err
	}
	for _, media := range mediaStatus.Status {
		a.media = &media
		a.volumeMedia = &media.Volume
	}
	return nil
}

func (a *Application) Close(stopMedia bool) error {
	if stopMedia {
		a.sendMediaConn(&cast.CloseHeader)
		a.sendDefaultConn(&cast.CloseHeader)
	}
	return a.conn.Close()
}

func (a *Application) Status() (*cast.Application, *cast.Media, *cast.Volume) {
	return a.application, a.media, a.volumeReceiver
}

func (a *Application) Pause() error {
	if a.media == nil {
		return ErrNoMediaPause
	}
	return a.sendMediaRecv(&cast.MediaHeader{
		PayloadHeader:  cast.PauseHeader,
		MediaSessionId: a.media.MediaSessionId,
	})
}

func (a *Application) Unpause() error {
	if a.media == nil {
		return ErrNoMediaUnpause
	}
	return a.sendMediaRecv(&cast.MediaHeader{
		PayloadHeader:  cast.PlayHeader,
		MediaSessionId: a.media.MediaSessionId,
	})
}

func (a *Application) StopMedia() error {
	if a.media == nil {
		return ErrNoMediaStop
	}
	return a.sendMediaRecv(&cast.MediaHeader{
		PayloadHeader:  cast.StopHeader,
		MediaSessionId: a.media.MediaSessionId,
	})
}

func (a *Application) Stop() error {
	return a.sendDefaultRecv(&cast.StopHeader)
}

func (a *Application) Next() error {
	if a.media == nil {
		return ErrNoMediaNext
	}

	return a.sendMediaRecv(&cast.QueueUpdate{
		PayloadHeader:  cast.QueueUpdateHeader,
		MediaSessionId: a.media.MediaSessionId,
		Jump:           1,
	})
}

func (a *Application) Previous() error {
	if a.media == nil {
		return ErrNoMediaPrevious
	}

	return a.sendMediaRecv(&cast.QueueUpdate{
		PayloadHeader:  cast.QueueUpdateHeader,
		MediaSessionId: a.media.MediaSessionId,
		Jump:           -1,
	})
}

func (a *Application) Skip() error {

	if a.media == nil {
		return ErrNoMediaSkip
	}

	a.updateMediaStatus()

	v := a.media.CurrentTime - 10
	if a.media.Media.Duration > 0 {
		v = a.media.Media.Duration - 10
	}

	return a.Seek(int(v))
}

func (a *Application) Seek(value int) error {
	if a.media == nil {
		return ErrMediaNotYetInitialised
	}

	if "CC1AD845" == a.application.AppId {
		absolute := a.media.CurrentTime + float32(value)
		return a.SeekToTime(absolute)
	}

	return a.sendMediaRecv(&cast.MediaHeader{
		PayloadHeader:  cast.SeekHeader,
		MediaSessionId: a.media.MediaSessionId,
		RelativeTime:   float32(value),
		ResumeState:    "PLAYBACK_START",
	})
}

func (a *Application) SeekFromStart(value int) error {
	if a.media == nil {
		return ErrMediaNotYetInitialised
	}

	// Get the latest media status
	a.updateMediaStatus()

	return a.sendMediaRecv(&cast.MediaHeader{
		PayloadHeader:  cast.SeekHeader,
		MediaSessionId: a.media.MediaSessionId,
		CurrentTime:    float32(value),
		ResumeState:    "PLAYBACK_START",
	})
}

func (a *Application) SeekToTime(value float32) error {
	if a.media == nil {
		return ErrMediaNotYetInitialised
	}

	return a.sendMediaRecv(&cast.MediaHeader{
		PayloadHeader:  cast.SeekHeader,
		MediaSessionId: a.media.MediaSessionId,
		CurrentTime:    value,
		ResumeState:    "PLAYBACK_START",
	})
}

func (a *Application) SetVolume(value float32) error {
	if value > 1 || value < 0 {
		return ErrVolumeOutOfRange
	}

	return a.sendDefaultRecv(&cast.SetVolume{
		PayloadHeader: cast.VolumeHeader,
		Volume: cast.Volume{
			Level: value,
		},
	})
}

func (a *Application) SetMuted(value bool) error {
	return a.sendDefaultRecv(&cast.SetVolume{
		PayloadHeader: cast.VolumeHeader,
		Volume: cast.Volume{
			Muted: value,
		},
	})
}

func (a *Application) getMediaStatus() (*cast.MediaStatusResponse, error) {
	apiMessage, err := a.sendAndWaitMediaRecv(&cast.GetStatusHeader)
	if err != nil {
		return nil, err
	}
	var response cast.MediaStatusResponse
	if err = json.Unmarshal([]byte(*apiMessage.PayloadUtf8), &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (a *Application) getReceiverStatus() (*cast.ReceiverStatusResponse, error) {
	apiMessage, err := a.sendAndWaitDefaultRecv(&cast.GetStatusHeader)
	if err != nil {
		return nil, err
	}
	var response cast.ReceiverStatusResponse
	if err := json.Unmarshal([]byte(*apiMessage.PayloadUtf8), &response); err != nil {
		return nil, err
	}
	return &response, nil

}

func (a *Application) PlayableMediaType(filename string) bool {
	if a.knownFileType(filename) {
		return true
	}

	switch path.Ext(filename) {
	case ".avi":
		return true
	}

	return false
}

func (a *Application) possibleContentType(filename string) (string, error) {
	// If filename is a URL returns the content-type from the HEAD response headers
	// Otherwise returns the content-type thanks to filetype package
	var contentType string

	match, err := filetype.MatchFile(filename)
	if err != nil {
		return "", errMsg(err, "match failed")
	}
	contentType = match.MIME.Value
	if contentType == "" {

		switch ext := strings.ToLower(path.Ext(filename)); ext {
		case ".jpg", ".jpeg":
			return "image/jpeg", nil
		case ".gif":
			return "image/gif", nil
		case ".bmp":
			return "image/bmp", nil
		case ".png":
			return "image/png", nil
		case ".webp":
			return "image/webp", nil
		case ".mp4", ".m4a", ".m4p":
			return "video/mp4", nil
		case ".webm":
			return "video/webm", nil
		case ".mp3":
			return "audio/mp3", nil
		case ".flac":
			return "audio/flac", nil
		case ".wav":
			return "audio/wav", nil
		case ".m3u8":
			return "application/x-mpegURL", nil
		default:
			return "", fmt.Errorf("unknown file extension %q", ext)
		}
	}
	return contentType, nil
}

func (a *Application) knownFileType(filename string) bool {
	if ct, _ := a.possibleContentType(filename); ct != "" {
		return true
	}
	return false
}

func (a *Application) castPlayableContentType(contentType string) bool {
	// https://developers.google.com/cast/docs/media
	switch contentType {
	case "image/apng", "image/bmp", "image/gif", "image/jpeg", "image/png", "image/webp":
		return true
	case "audio/mp2t", "audio/mp3", "audio/mp4", "audio/ogg", "audio/wav", "audio/webm":
		return true
	case "video/mp4", "video/webm":
		return true
	}

	return false
}

func (a *Application) Load(file, contentType string, transcode bool) error {
	var mi mediaItem
	mediaItems, err := a.loadAndServeFiles([]string{file}, contentType, transcode)
	if err != nil {
		return err
	}

	if len(mediaItems) != 1 {
		return fmt.Errorf("was expecting 1 media item, received %d", len(mediaItems))
	}
	mi = mediaItems[0]

	if err = a.ensureIsDefaultMediaReceiver(); err != nil {
		return err
	}

	// NOTE: This isn't concurrent safe, but it doesn't need to be at the moment!
	a.MediaStart()

	// Send the command to the chromecast
	a.sendMediaRecv(&cast.LoadMediaCommand{
		PayloadHeader: cast.LoadHeader,
		CurrentTime:   0,
		Autoplay:      true,
		Media: cast.MediaItem{
			ContentId:   mi.contentURL,
			StreamType:  "BUFFERED",
			ContentType: mi.contentType,
		},
	})

	// Wait until we have been notified that the media has finished playing
	a.MediaWait()
	return nil
}

//
//func (a *Application) LoadApp(appID, contentID string) error {
//	// old list https://gist.github.com/jloutsenhizer/8855258.
//	// NOTE: This isn't concurrent safe, but it doesn't need to be at the moment!
//	a.MediaStart()
//
//	if err := a.ensureIsAppID(appID); err != nil {
//		return errors.Wrapf(err, "unable to change chromecast app")
//	}
//
//	// Send the command to the chromecast
//	a.sendMediaRecv(&cast.LoadMediaCommand{
//		PayloadHeader: cast.LoadHeader,
//		CurrentTime:   0,
//		Autoplay:      true,
//		Media: cast.MediaItem{
//			ContentId:  contentID,
//			StreamType: "BUFFERED",
//		},
//	})
//
//	// Wait until we have been notified that the media has finished playing
//	a.MediaWait()
//
//	return nil
//}

func (a *Application) QueueLoad(filenames []string, contentType string, transcode bool) error {

	mediaItems, err := a.loadAndServeFiles(filenames, contentType, transcode)
	if err != nil {
		return err
	}

	if err = a.ensureIsDefaultMediaReceiver(); err != nil {
		return err
	}

	items := make([]cast.QueueLoadItem, len(mediaItems))
	for i, mi := range mediaItems {
		items[i] = cast.QueueLoadItem{
			Autoplay:         true,
			PlaybackDuration: 60,
			Media: cast.MediaItem{
				ContentId:   mi.contentURL,
				StreamType:  "BUFFERED",
				ContentType: mi.contentType,
			},
		}
	}

	// Send the command to the chromecast
	a.sendMediaRecv(&cast.QueueLoad{
		PayloadHeader: cast.QueueLoadHeader,
		CurrentTime:   0,
		StartIndex:    0,
		RepeatMode:    "REPEAT_OFF",
		Items:         items,
	})

	// Wait until we have been notified that the media has finished playing
	// This never returns; blocks forever.
	a.MediaWait()
	return nil
}

func (a *Application) ensureIsDefaultMediaReceiver() error {
	// If the current chromecast application isn't the Default Media Receiver
	// we need to change it.
	return a.ensureIsAppID(defaultChromecastAppID)
}

func (a *Application) ensureIsAppID(appID string) error {
	if a.application == nil || a.application.AppId != appID {
		_, err := a.sendAndWaitDefaultRecv(&cast.LaunchRequest{
			PayloadHeader: cast.LaunchHeader,
			AppId:         appID,
		})

		if err != nil {
			return errMsg(err, fmt.Sprintf("unable to change to appID %q", appID))
		}
		// Update the 'application' and 'media' field on the 'CastApplication'
		return a.Update()
	}
	return nil
}

type mediaItem struct {
	filename    string
	contentType string
	contentURL  string
	transcode   bool
}

func (a *Application) loadAndServeFiles(filenames []string, contentType string, transcode bool) ([]mediaItem, error) {
	mediaItems := make([]mediaItem, len(filenames))
	for i, filename := range filenames {
		transcodeFile := transcode
		if _, err := os.Stat(filename); err != nil {
			return nil, errMsg(err, fmt.Sprintf("unable to find %q", filename))
		}
		/*
			We can play media for the following:

			- if we have a filename with a known content type
			- if we have a filename, and a specified contentType
			- if we have a filename with an unknown content type, and transcode is true
			-
		*/
		knownFileType := a.knownFileType(filename)
		if !knownFileType && contentType == "" && !transcodeFile {
			return nil, fmt.Errorf("unknown content-type for %q, either specify a content-type or set transcode to true", filename)
		}

		// If we have a content-type specified we should always
		// attempt to use that
		contentTypeToUse := contentType
		if contentType != "" {
		} else if knownFileType {
			// If this is a media file we know the chromecast can play,
			// then we don't need to transcode it.
			contentTypeToUse, _ = a.possibleContentType(filename)
			if a.castPlayableContentType(contentTypeToUse) {
				transcodeFile = false
			}
		} else if transcodeFile {
			contentTypeToUse = "video/mp4"
		}

		mediaItems[i] = mediaItem{
			filename:    filename,
			contentType: contentTypeToUse,
			transcode:   transcodeFile,
		}
		a.mediaFilenames = append(a.mediaFilenames, filename)
	}

	localIP, err := a.getLocalIP()
	if err != nil {
		return nil, err
	}
	a.log("local IP address: %s", localIP)

	a.log("starting streaming server...")
	// Start server to serve the media
	if err = a.startStreamingServer(); err != nil {
		return nil, errMsg(err, "unable to start streaming server")
	}
	a.log("started streaming server")

	for i, m := range mediaItems {
		mediaItems[i].contentURL = fmt.Sprintf("http://%s:%d?media_file=%s&live_streaming=%t",
			localIP, a.serverPort, m.filename, m.transcode)
	}

	return mediaItems, nil
}

func (a *Application) getLocalIP() (string, error) {
	if a.localIP != "" {
		return a.localIP, nil
	}
	return getLocalIp(), nil
}

func (a *Application) startStreamingServer() error {
	if a.httpServer != nil {
		return nil
	}
	a.log("trying to find available port to start streaming server on")

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(a.serverPort))
	if err != nil {
		return err
	}

	a.serverPort = listener.Addr().(*net.TCPAddr).Port
	a.log("found available port :%d", a.serverPort)

	a.httpServer = http.NewServeMux()

	a.httpServer.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check to see if we have a 'filename' and if it is one of the ones that have
		// already been validated and is useable.
		filename := r.URL.Query().Get("media_file")
		canServe := false
		for _, fn := range a.mediaFilenames {
			if fn == filename {
				canServe = true
			}
		}

		// Check to see if this is a live streaming video and we need to use an
		// infinite range request / response. This comes from media that is either
		// live or currently being transcoded to a different media format.
		liveStreaming := false
		if ls := r.URL.Query().Get("live_streaming"); ls == "true" {
			liveStreaming = true
		}

		a.log("canServe=%t, liveStreaming=%t, filename=%s", canServe, liveStreaming, filename)
		if canServe {
			if !liveStreaming {
				http.ServeFile(w, r, filename)
			} else {
				a.serveLiveStreaming(w, r, filename)
			}
		} else {
			http.Error(w, "Invalid file", 400)
		}
		a.log("method=%s, headers=%v, reponse_headers=%v", r.Method, r.Header, w.Header())
	})

	go func() {
		a.log("media server listening on %d", a.serverPort)
		if err = http.Serve(listener, a.httpServer); err != nil && err != http.ErrServerClosed {
			log.WithField("package", "application").WithError(err).Fatal("error serving HTTP")
		}
	}()

	return nil
}

func (a *Application) serveLiveStreaming(w http.ResponseWriter, r *http.Request, filename string) {
	cmd := exec.Command(
		"ffmpeg",
		"-re", // encode at 1x playback speed, to not burn the CPU
		"-i", filename,
		"-vcodec", "h264",
		"-acodec", "aac",
		"-ac", "2", // chromecasts don't support more than two audio channels
		"-f", "mp4",
		"-movflags", "frag_keyframe+faststart",
		"-strict", "-experimental",
		"pipe:1",
	)

	cmd.Stdout = w
	if a.debug {
		cmd.Stderr = os.Stderr
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Transfer-Encoding", "chunked")

	if err := cmd.Run(); err != nil {
		log.WithField("package", "application").WithFields(log.Fields{
			"filename": filename,
		}).WithError(err).Error("error transcoding")
	}
}

func (a *Application) log(message string, args ...interface{}) {
	if a.debug {
		log.WithField("package", "application").Infof(message, args...)
	}
}

func (a *Application) send(payload cast.Payload, sourceID, destinationID, namespace string) (int, error) {
	// NOTE: Not concurrent safe, but currently only synchronous flow is possible
	// TODO(vishen): just make concurrent safe regardless of current flow
	requestID += 1
	payload.SetRequestId(requestID)
	return requestID, a.conn.Send(requestID, payload, sourceID, destinationID, namespace)
}

func (a *Application) sendAndWait(payload cast.Payload, sourceID, destinationID, namespace string) (*pb.CastMessage, error) {
	requestID, err := a.send(payload, sourceID, destinationID, namespace)
	if err != nil {
		return nil, err
	}

	// Set a timeout to wait for the response
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	// TODO(vishen): not concurrent safe. Not a problem at the moment
	// because only synchronous flow currently allowed.
	resultChan := make(chan *pb.CastMessage, 1)
	a.mapLock.Lock()
	a.resultChanMap[requestID] = resultChan
	a.mapLock.Unlock()
	defer func() {
		a.mapLock.Lock()
		delete(a.resultChanMap, requestID)
		a.mapLock.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultChan:
		return result, nil
	}
}

// TODO(vishen): needing send(AndWait)* method seems a bit clunky, is there a better approach?
// Maybe having a struct that has send and sendAndWait, similar to before.
func (a *Application) sendDefaultConn(payload cast.Payload) error {
	_, err := a.send(payload, defaultSender, defaultRecv, namespaceConn)
	return err
}

func (a *Application) sendDefaultRecv(payload cast.Payload) error {
	_, err := a.send(payload, defaultSender, defaultRecv, namespaceRecv)
	return err
}

func (a *Application) sendMediaConn(payload cast.Payload) error {
	if a.application == nil {
		return ErrApplicationNotSet
	}
	_, err := a.send(payload, defaultSender, a.application.TransportId, namespaceConn)
	return err
}

func (a *Application) sendMediaRecv(payload cast.Payload) error {
	if a.application == nil {
		return ErrApplicationNotSet
	}
	_, err := a.send(payload, defaultSender, a.application.TransportId, namespaceMedia)
	return err
}

func (a *Application) sendAndWaitDefaultConn(payload cast.Payload) (*pb.CastMessage, error) {
	return a.sendAndWait(payload, defaultSender, defaultRecv, namespaceConn)
}

func (a *Application) sendAndWaitDefaultRecv(payload cast.Payload) (*pb.CastMessage, error) {
	return a.sendAndWait(payload, defaultSender, defaultRecv, namespaceRecv)
}

func (a *Application) sendAndWaitMediaConn(payload cast.Payload) (*pb.CastMessage, error) {
	if a.application == nil {
		return nil, ErrApplicationNotSet
	}
	return a.sendAndWait(payload, defaultSender, a.application.TransportId, namespaceConn)
}

func (a *Application) sendAndWaitMediaRecv(payload cast.Payload) (*pb.CastMessage, error) {
	if a.application == nil {
		return nil, ErrApplicationNotSet
	}
	return a.sendAndWait(payload, defaultSender, a.application.TransportId, namespaceMedia)
}

func (a *Application) startTranscodingServer(command string) error {
	if a.httpServer != nil {
		return nil
	}
	a.log("trying to find available port to start transcoding server on")

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return errMsg(err, "unable to bind to local tcp address")
	}

	a.serverPort = listener.Addr().(*net.TCPAddr).Port
	a.log("found available port :%d", a.serverPort)

	a.httpServer = http.NewServeMux()

	a.httpServer.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check to see if we have a 'filename' and if it is one of the ones that have
		// already been validated and is useable.
		filename := r.URL.Query().Get("media_file")
		canServe := false
		for _, fn := range a.mediaFilenames {
			if fn == filename {
				canServe = true
			}
		}

		a.log("canServe=%t, liveStreaming=%t, filename=%s", canServe, true, filename)
		if canServe {
			args := strings.Split(command, " ")
			cmd := exec.Command(args[0], args[1:]...)

			cmd.Stdout = w
			if a.debug {
				cmd.Stderr = os.Stderr
			}

			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Transfer-Encoding", "chunked")

			if err := cmd.Run(); err != nil {
				log.WithField("package", "application").WithFields(log.Fields{
					"filename": filename,
				}).WithError(err).Error("error transcoding")
			}
		} else {
			http.Error(w, "Invalid file", 400)
		}
		a.log("method=%s, headers=%v, reponse_headers=%v", r.Method, r.Header, w.Header())

	})

	go func() {
		a.log("media server listening on %d", a.serverPort)
		if err := http.Serve(listener, a.httpServer); err != nil && err != http.ErrServerClosed {
			log.WithField("package", "application").WithError(err).Fatal("error serving HTTP")
		}
	}()

	return nil
}

func (a *Application) Transcode(command string, contentType string) error {

	if command == "" || contentType == "" {
		return fmt.Errorf("command and content-type flags needs to be set when transcoding")
	}

	filename := "pipe_output"
	// Add the filename to the list of filenames that go-chromecast will serve.
	a.mediaFilenames = append(a.mediaFilenames, filename)

	localIP, err := a.getLocalIP()
	if err != nil {
		return err
	}
	a.log("local IP address: %s", localIP)

	a.log("starting transcoding server...")
	// Start server to serve the media
	if err = a.startTranscodingServer(command); err != nil {
		return errMsg(err, "unable to start transcoding server")
	}
	a.log("started transcoding server")

	// We can only set the content url after the server has started, otherwise we have
	// no way to know the port used.
	contentURL := fmt.Sprintf("http://%s:%d?media_file=%s", localIP, a.serverPort, filename)

	if err = a.ensureIsDefaultMediaReceiver(); err != nil {
		return err
	}

	// NOTE: This isn't concurrent safe, but it doesn't need to be at the moment!
	a.MediaStart()

	// Send the command to the chromecast
	a.sendMediaRecv(&cast.LoadMediaCommand{
		PayloadHeader: cast.LoadHeader,
		CurrentTime:   0,
		Autoplay:      true,
		Media: cast.MediaItem{
			ContentId:   contentURL,
			StreamType:  "BUFFERED",
			ContentType: contentType,
		},
	})

	// Wait until we have been notified that the media has finished playing
	a.MediaWait()
	return nil
}

var (
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	getLocalIp = base.GetLocalIp
	errMsg     = base.ErrMsg
)
