package main

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/jerblack/server_tools/base"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//register
//dynamically register and unregister containers in dns as they are started and stopped
//	 monitor docker engine for container start and stop events
//    register in dns on start, unregister on stop
//  bridge and macvlan containers register A and PTR record
//  host-network containers register cname for docker host
//  non-network containers are skipped
//	uses linux utility nsupdate to handle dns registration
// unregister all on app shutdown
//
// configure with environment variables
//	DOCKER_HOST				-> url of docker server, blank for unix:///var/run/docker.sock
// DOCKER_API_VERSION		-> docker api version to target, blank for latest
// DOCKER_CERT_PATH		-> path to load docker TLS certificates from
// DOCKER_TLS_VERIFY		-> enable or disable TLS verification
// REGISTER_DNS_SERVER		-> dns server to register with, blank for system resolver
// REGISTER_DOMAIN_NAME	-> dns domain to register. zone should exist on server and set to allow updates, blank for "home"
// REGISTER_PTR		    -> register PTR record in reverse lookup zone. true or false, blank for true
// REGISTER_TTL			-> TTL (in seconds) of records when registering, blank for 3600
// REGISTER_CLEANUP		-> unregister dns records registered by this service on shutdown of service
//							   true or false, blank for true

var (
	dnsServer   string
	domain      = "home"
	ttl         = 3600
	regEvents   = []string{"start", "restart", "unpause"}
	unRegEvents = []string{"kill", "die", "stop", "pause"}
	cleanup     = true
	registerPtr = true
)

func getEnv() {
	env := os.Getenv("REGISTER_DNS_SERVER")
	if env != "" {
		dnsServer = env
	}
	env = os.Getenv("REGISTER_DOMAIN_NAME")
	if env != "" {
		domain = env
	}
	env = os.Getenv("REGISTER_PTR")
	if env != "" {
		if isAny(strings.ToLower("env"), "true", "yes", "1") {
			registerPtr = true
		} else if isAny(strings.ToLower("env"), "false", "no", "0") {
			registerPtr = false
		} else {
			p("invalid value \"%s\" for REGISTER_PTR environment variable. must be any of true, false, yes, no, 1 or 0", env)
			os.Exit(1)
		}
	}
	env = os.Getenv("REGISTER_TTL")
	if env != "" {
		n, e := strconv.Atoi(env)
		if e != nil {
			chk(e)
			p("invalid value \"%s\" for REGISTER_TTL environment variable. must be integer whole number", env)
			os.Exit(1)
		}
		ttl = n
	}
	env = os.Getenv("REGISTER_CLEANUP")
	if env != "" {
		if isAny(strings.ToLower("env"), "true", "yes", "1") {
			cleanup = true
		} else if isAny(strings.ToLower("env"), "false", "no", "0") {
			cleanup = false
		} else {
			p("invalid value \"%s\" for REGISTER_CLEANUP environment variable. must be any of true, false, yes, no, 1 or 0", env)
			os.Exit(1)
		}
	}
}

func main() {
	getEnv()
	d := Docker{}
	d.start()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(
		signalChan,
		syscall.SIGHUP,  // kill -SIGHUP XXXX
		syscall.SIGINT,  // kill -SIGINT XXXX or Ctrl+c
		syscall.SIGQUIT, // kill -SIGQUIT XXXX
	)
	<-signalChan
	p("exiting. doing cleanup.")
	if !cleanup {
		return
	}
	for _, h := range d.Hosts {
		h.unregister()
	}
}

type Docker struct {
	Client *client.Client
	Hosts  map[string]*Host
	Name   string
}

func (d *Docker) start() {
	var e error
	d.Client, e = client.NewClientWithOpts(client.FromEnv)
	chkFatal(e)
	d.Hosts = make(map[string]*Host)
	go d.events()
	go d.checkExisting()
}

func (d *Docker) getName() string {
	if d.Name != "" {
		return d.Name
	}
	i, _ := d.Client.Info(context.Background())
	d.Name = getFqdn(i.Name)
	return d.Name
}

func (d *Docker) checkExisting() {
	for {
		containers, e := d.Client.ContainerList(context.Background(), types.ContainerListOptions{})
		chkFatal(e)
		for _, c := range containers {
			h, ok := d.Hosts[c.ID]
			if !ok {
				p("preexisting container found. registering %s", c.Names[0])
				h = d.inspect(c.ID)
				h.register()
			}
		}
		time.Sleep(60 * time.Minute)
	}
}

func (d *Docker) events() {
	filter := filters.NewArgs(
		filters.Arg("type", "container"),
		filters.Arg("event", "start"),
		filters.Arg("event", "kill"),
		filters.Arg("event", "die"),
		filters.Arg("event", "stop"),
		filters.Arg("event", "restart"),
		filters.Arg("event", "unpause"),
		filters.Arg("event", "pause"),
	)

	msgs, errs := d.Client.Events(context.Background(), types.EventsOptions{
		Filters: filter,
	})
	for {
		select {
		case msg := <-msgs:
			h, ok := d.Hosts[msg.ID]
			if isAny(msg.Status, regEvents...) {
				if !ok {
					h = d.inspect(msg.ID)
					h.register()
				}
			} else {
				if ok {
					h.unregister()
					delete(d.Hosts, msg.ID)
				}
			}
			p("event -> type %s | id %s | status %s", msg.Type, msg.ID, msg.Status)
		case e := <-errs:
			p("error received: %s", e.Error())
			return
		}
	}
}

func (d *Docker) inspect(id string) *Host {
	c, e := d.Client.ContainerInspect(context.Background(), id)
	chkFatal(e)
	h := Host{
		Host:     getFqdn(c.Config.Hostname),
		Ip:       c.NetworkSettings.IPAddress,
		Id:       id,
		Register: true,
	}
	p("container network mode for %s is %s", c.Name, c.HostConfig.NetworkMode)
	if c.HostConfig.NetworkMode == "host" {
		h.Alias = d.getName()
	} else if c.NetworkSettings.IPAddress == "" {
		h.Register = false
	}
	d.Hosts[id] = &h
	return &h
}

type Host struct {
	Host, Id string
	Ip       string
	Alias    string
	Register bool
}

func (h *Host) unregA() {
	p("unregistering A record: %s -> %s", h.Host, h.Ip)
	args := fmt.Sprintf("update del %s A %s\nsend\nquit\n", h.Host, h.Ip)
	runNsUpdate(args)
}
func (h *Host) unregPtr() {
	if !registerPtr {
		return
	}
	p("unregistering PTR record: %s -> %s", getPtr(h.Ip), h.Host)
	args := fmt.Sprintf("update del %s PTR %s\nsend\nquit\n", getPtr(h.Ip), h.Host)
	runNsUpdate(args)
}
func (h *Host) unregCname() {
	p("unregistering CNAME record: %s -> %s", h.Host, h.Alias)
	args := fmt.Sprintf("update del %s CNAME %s\nsend\nquit\n", h.Host, h.Alias)
	runNsUpdate(args)
}
func (h *Host) regA() {
	p("registering A record: %s -> %s", h.Host, h.Ip)
	args := fmt.Sprintf("update add %s %d IN A %s\nsend\nquit\n", h.Host, ttl, h.Ip)
	runNsUpdate(args)
}
func (h *Host) regPtr() {
	if !registerPtr {
		return
	}
	p("registering PTR record: %s -> %s", getPtr(h.Ip), h.Host)
	args := fmt.Sprintf("update add %s %d IN PTR %s\nsend\nquit\n", getPtr(h.Ip), ttl, h.Host)
	runNsUpdate(args)
}
func (h *Host) regCname() {
	p("registering CNAME record: %s -> %s", h.Host, h.Alias)
	args := fmt.Sprintf("update add %s %d IN CNAME %s\nsend\nquit\n", h.Host, ttl, h.Alias)
	runNsUpdate(args)
}

func (h *Host) unregister() {
	if !h.Register {
		return
	}

	if h.Alias != "" {
		h.unregCname()
	} else {
		h.unregPtr()
		h.unregA()
	}
}
func (h *Host) register() {
	if !h.Register {
		return
	}
	if h.Alias != "" {
		h.regCname()
	} else {
		h.regA()
		h.regPtr()
	}
}

func runNsUpdate(args string) {
	if dnsServer != "" {
		args = fmt.Sprintf("server %s\n%s", dnsServer, args)
	}
	cmd := exec.Command("nsupdate")
	in, e := cmd.StdinPipe()
	if e != nil {
		panic(e)
	}
	go func() {
		defer in.Close()
		_, e = in.Write([]byte(args))
		if e != nil {
			panic(e)
		}
	}()
	e = cmd.Run()
	if e != nil {
		panic(e)
	}
}
func getFqdn(host string) string {
	if strings.Contains(host, ".") {
		return host
	} else {
		return fmt.Sprintf("%s.%s", host, domain)
	}
}
func getPtr(ip string) string {
	if ip == "" {
		return ""
	}
	o := strings.Split(ip, ".")
	return fmt.Sprintf("%s.%s.%s.%s.in-addr.arpa", o[3], o[2], o[1], o[0])
}

var (
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	isAny      = base.IsAny
	getLocalIp = base.GetLocalIp
	dnsQuery   = base.DnsQueryServer
)
