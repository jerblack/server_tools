package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/jerblack/server_tools/base"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
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
// DOCKER_HOST				-> url of docker server, blank for unix:///var/run/docker.sock
// DOCKER_API_VERSION		-> docker api version to target, blank for latest
// DOCKER_CERT_PATH		-> path to load docker TLS certificates from
// DOCKER_TLS_VERIFY		-> enable or disable TLS verification
// REGISTER_DNS_SERVER		-> dns server to register with, blank for system resolver
// REGISTER_DOMAIN_NAME	-> dns domain to register. zone should exist on server and set to allow updates, blank for "home"
// REGISTER_PTR		    -> register PTR record in reverse lookup zone. true or false, blank for true
// REGISTER_TTL			-> TTL (in seconds) of records when registering, blank for 3600
// REGISTER_CLEANUP		-> unregister dns records registered by this service on shutdown of service
//							   true or false, blank for true
// REGISTER_CONFIG		-> path to config file including file name, blank for /etc/register.conf

var (
	dnsServer     string
	domain        = "home"
	ttl           = 3600
	regEvents     = []string{"start", "restart", "unpause"}
	unRegEvents   = []string{"kill", "die", "stop", "pause"}
	regExclusions []string
	cleanup       = true
	registerPtr   = true
	confFilePath  = "/etc/register.conf"

	forwardServer string
	forwards      map[string][]*Forward
)

func getEnv() {
	reTrue := regexp.MustCompile(`(?i)^(true|1|yes)$`)
	reFalse := regexp.MustCompile(`(?i)^(false|0|no)$`)
	exit := false
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
		if !reTrue.MatchString(env) && !reFalse.MatchString(env) {
			p("config: invalid value for REGISTER_PTR: %s. use true or false", env)
			exit = true
		}
		registerPtr = reTrue.MatchString(env)
	}
	env = os.Getenv("REGISTER_TTL")
	if env != "" {
		n, e := strconv.Atoi(env)
		if e != nil {
			chk(e)
			p("invalid value \"%s\" for REGISTER_TTL environment variable. must be integer whole number", env)
			exit = true
		}
		ttl = n
	}
	env = os.Getenv("REGISTER_CLEANUP")
	if env != "" {
		if !reTrue.MatchString(env) && !reFalse.MatchString(env) {
			p("config: invalid value for REGISTER_CLEANUP: %s. use true or false", env)
			exit = true
		}
		cleanup = reTrue.MatchString(env)
	}
	env = os.Getenv("REGISTER_CONFIG")
	if env != "" {
		if !fileExists(env) {
			p("file specified in environment variable REGISTER_CONFIG does not exist: %s", env)
			exit = true
		}
		confFilePath = env
	}
	if exit {
		os.Exit(1)
	}
}

func parseConfig() {
	forwards = make(map[string][]*Forward)
	if !fileExists(confFilePath) {
		return
	}
	var config string
	b, e := os.ReadFile(confFilePath)
	chkFatal(e)
	config = string(b)

	lines := strings.Split(config, "\n")
	reEq := regexp.MustCompile(`\s*=\s*`)
	reTrue := regexp.MustCompile(`(?i)^(true|1|yes)$`)
	reFalse := regexp.MustCompile(`(?i)^(false|0|no)$`)
	reSpaces := regexp.MustCompile(`\s+`)
	reForward := regexp.MustCompile(`(?i)^[\d\w-_]+\s+(tcp|udp)\s+\d+\s+\d+$`)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		r := strings.NewReplacer("\"", "", "'", "")
		line = r.Replace(line)
		line = reEq.ReplaceAllString(line, "=")

		kv := strings.Split(line, "=")
		k, v := kv[0], kv[1]
		exit := false
		switch k {
		case "docker-host":
			if v != "" {
				e = os.Setenv("DOCKER_HOST", v)
				chkFatal(e)
			}
		case "docker-api-version":
			if v != "" {
				e = os.Setenv("DOCKER_API_VERSION", v)
				chkFatal(e)
			}
		case "docker-cert-path":
			if v != "" {
				e = os.Setenv("DOCKER_CERT_PATH", v)
				chkFatal(e)
			}
		case "docker-tls-verify":
			if v != "" {
				e = os.Setenv("DOCKER_TLS_VERIFY", v)
				chkFatal(e)
			}
		case "register-dns-server":
			if v != "" {
				if !isIp(v) {
					p("config: invalid ip set for register-dns-server: %s", v)
					exit = true
				}
				dnsServer = v
			}
		case "register-domain-name":
			if v != "" {
				domain = v
			}
		case "register-ptr":
			if v != "" {
				if !reTrue.MatchString(v) && !reFalse.MatchString(v) {
					p("config: invalid value for register-ptr: %s. use true or false", v)
					exit = true
				}
				registerPtr = reTrue.MatchString(v)
			}
		case "register-ttl":
			if v != "" {
				ttl, e = strconv.Atoi(v)
				chk(e)
				if e != nil {
					p("config: invalid value for register-ttl: %s. use whole number", v)
					exit = true
				}
			}
		case "register-cleanup":
			if v != "" {
				if !reTrue.MatchString(v) && !reFalse.MatchString(v) {
					p("config: invalid value for register-cleanup: %s. use true or false", v)
					exit = true
				}
				cleanup = reTrue.MatchString(v)
			}
		case "register-exclude":
			if v != "" {
				v = strings.ToLower(v)
				regExclusions = append(regExclusions, v)
			}
		case "forward-server":
			if v != "" {
				forwardServer = fmt.Sprintf("http://%s/cmd", v)
			}
		case "forward":
			if v != "" {
				if !reForward.MatchString(v) {
					p("config: invalid forward specification: %s. "+
						"use -> <hostname> <tcp or udp> <external port> <internal port>", v)
					exit = true
				}
				v = reSpaces.ReplaceAllString(v, " ")
				f := getForward(v)
				forwards[getFqdn(f.Host)] = append(forwards[getFqdn(f.Host)], f)
			}
		}
		if exit {
			os.Exit(1)
		}
	}
}

func main() {
	getEnv()
	parseConfig()
	d := Docker{}
	d.start()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(
		signalChan,
		syscall.SIGHUP,  // kill -SIGHUP XXXX
		syscall.SIGINT,  // kill -SIGINT XXXX or Ctrl+c
		syscall.SIGQUIT, // kill -SIGQUIT XXXX
		syscall.SIGTERM, // shutdown service
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
				host := c.Names[0]
				if isHostExcluded(host) {
					p("preexisting container found but in exclusion list. skipping registration: %s", host)
				} else {
					p("preexisting container found. registering %s", host)
					h = d.inspect(c.ID)
					h.register()
				}
			}
		}
		time.Sleep(60 * time.Minute)
	}
}

func isHostExcluded(host string) bool {
	host = strings.TrimPrefix(host, "/")
	host = strings.TrimSuffix(host, "."+domain)
	host = strings.ToLower(host)
	return isAny(host, regExclusions...)
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
			p("event -> type %s | id %s | status %s", msg.Type, msg.ID, msg.Status)
			if isAny(msg.Status, regEvents...) {
				if !ok {
					h = d.inspect(msg.ID)
					if isHostExcluded(h.Host) {
						p("ignoring event for excluded host: %s", h.Host)
					} else {
						h.register()
					}
				}
			} else {
				if ok {
					if isHostExcluded(h.Host) {
						p("ignoring event for excluded host: %s", h.Host)
					} else {
						h.unregister()
						delete(d.Hosts, msg.ID)
					}
				}
			}
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
		ip := c.NetworkSettings.Networks[string(c.HostConfig.NetworkMode)].IPAddress
		if ip != "" {
			h.Ip = ip
		} else {
			h.Register = false
		}
	}
	f, ok := forwards[h.Host]
	if ok {
		h.Forwards = append(h.Forwards, f...)
	}

	d.Hosts[id] = &h
	return &h
}

func getForward(rule string) *Forward {
	r := strings.Split(rule, " ")
	f := Forward{
		Host:    r[0],
		Proto:   r[1],
		ExtPort: r[2],
		IntPort: r[3],
	}
	return &f
}

type Forward struct {
	Action  string `json:"action"`
	Host    string `json:"host"`
	Proto   string `json:"proto"`
	ExtPort string `json:"ext_port"`
	IntPort string `json:"int_port"`
	Ip      string `json:"ip"`
}

func (f *Forward) String() string {
	return fmt.Sprintf("%s: %s %s -> %s:%s", f.Host, f.Proto, f.ExtPort, f.Ip, f.IntPort)
}
func (f *Forward) enable(ip string) {
	f.Action = "enable"
	f.Ip = ip
	b, e := json.Marshal(f)
	chkFatal(e)
	rsp, e := http.Post(forwardServer, "application/json", bytes.NewBuffer(b))
	chk(e)
	if e != nil || rsp.StatusCode != http.StatusOK {
		p("failed to enable forward port -> %s", f)
	} else {
		p("enabled port forward -> %s", f)
	}
}
func (f *Forward) disable(ip string) {
	f.Action = "disable"
	f.Ip = ip
	b, e := json.Marshal(f)
	chkFatal(e)
	rsp, e := http.Post(forwardServer, "application/json", bytes.NewBuffer(b))
	chk(e)
	if e != nil || rsp.StatusCode != http.StatusOK {
		p("failed to disable forward port -> %s", f)
	} else {
		p("disabled port forward -> %s", f)
	}
}

type Host struct {
	Host, Id string
	Ip       string
	Alias    string
	Register bool
	Forwards []*Forward
}

func (h *Host) unregA() {
	p("unregistering A record: %s -> %s", h.Host, h.Ip)
	args := fmt.Sprintf("update del %s A %s", h.Host, h.Ip)
	runNsUpdate(args)
}
func (h *Host) unregPtr() {
	if !registerPtr {
		return
	}
	p("unregistering PTR record: %s -> %s", getPtr(h.Ip), h.Host)
	args := fmt.Sprintf("update del %s PTR %s", getPtr(h.Ip), h.Host)
	runNsUpdate(args)
}
func (h *Host) unregCname() {
	p("unregistering CNAME record: %s -> %s", h.Host, h.Alias)
	args := fmt.Sprintf("update del %s CNAME %s", h.Host, h.Alias)
	runNsUpdate(args)
}

func (h *Host) cleanupA() {
	p("cleanup: querying DNS for existing A records for host: %s", h.Host)
	ips := dnsQueryA(h.Host, dnsServer)
	if len(ips) > 0 {
		p("cleanup: found %d existing A records for host %s", len(ips), h.Host)
		for _, ip := range ips {
			p("cleanup: unregistering A record: %s -> %s", h.Host, ip)
			args := fmt.Sprintf("update del %s A %s", h.Host, ip)
			runNsUpdate(args)
		}
	} else {
		p("cleanup: no existing A records found for host %s", h.Host)
	}
}

func (h *Host) cleanupPtr() {
	p("cleanup: querying DNS for existing PTR records for IP: %s", h.Ip)
	hosts := dnsQueryPtr(h.Ip, dnsServer)
	if len(hosts) > 0 {
		p("cleanup: found %d existing PTR records for IP %s", len(hosts), h.Ip)
		for _, host := range hosts {
			p("unregistering PTR record: %s -> %s", getPtr(h.Ip), host)
			args := fmt.Sprintf("update del %s PTR %s", getPtr(h.Ip), host)
			runNsUpdate(args)
		}
	} else {
		p("cleanup: no existing PTR records found for host %s", h.Host)
	}
}

func (h *Host) regA() {
	h.cleanupA()
	p("registering A record: %s -> %s", h.Host, h.Ip)
	args := fmt.Sprintf("update add %s %d IN A %s", h.Host, ttl, h.Ip)
	runNsUpdate(args)
}
func (h *Host) regPtr() {
	h.cleanupPtr()
	if !registerPtr {
		return
	}
	p("registering PTR record: %s -> %s", getPtr(h.Ip), h.Host)
	args := fmt.Sprintf("update add %s %d IN PTR %s", getPtr(h.Ip), ttl, h.Host)
	runNsUpdate(args)
}
func (h *Host) regCname() {
	p("registering CNAME record: %s -> %s", h.Host, h.Alias)
	args := fmt.Sprintf("update add %s %d IN CNAME %s", h.Host, ttl, h.Alias)
	runNsUpdate(args)
}

func (h *Host) unregister() {
	if h.Register {
		if h.Alias != "" {
			h.unregCname()
		} else {
			h.unregPtr()
			h.unregA()
		}
	}
	if forwardServer != "" {
		for _, f := range h.Forwards {
			f.disable(h.Ip)
		}
	}
}
func (h *Host) register() {
	if h.Register {
		if h.Alias != "" {
			h.regCname()
		} else {
			h.regA()
			h.regPtr()
		}
	}
	if forwardServer != "" {
		for _, f := range h.Forwards {
			f.enable(h.Ip)
		}
	}

}

func runNsUpdate(arg string) {
	args := []string{
		"check-names off",
	}
	if dnsServer != "" {
		args = append(args, fmt.Sprintf("server %s", dnsServer))
	}
	args = append(args, arg, "send", "quit")
	cmd := exec.Command("nsupdate")
	in, e := cmd.StdinPipe()
	if e != nil {
		panic(e)
	}
	go func() {
		defer in.Close()
		_, e = in.Write([]byte(strings.Join(args, "\n") + "\n"))
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
	p           = base.P
	chk         = base.Chk
	chkFatal    = base.ChkFatal
	isAny       = base.IsAny
	getLocalIp  = base.GetLocalIp
	dnsQueryA   = base.DnsQueryServerA
	dnsQueryPtr = base.DnsQueryServerPtr
	fileExists  = base.FileExists
	isIp        = base.IsIp
)
