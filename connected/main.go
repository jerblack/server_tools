package main

import (
	"fmt"
	"github.com/jerblack/base"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

/*
	at startup
		copy all embedded wg/*.conf files to /etc/wireguard

	enumerate conf files in wireguard folder
		parse conf files into filename, basename, server localInIp

	iterate randomly through confs
		for each endpoint localInIp, create route through gateway

	randomly select first connection
		connect
		verify connected
		if connected, update cloudflare dns record with localInIp obtained from https://ipv4.am.i.mullvad.net

	monitor connection
		every minute ping heartbeat localInIp, on 3 successive fails go to next connection

*/

var (
	// conf folder can also be specified in CONFIG_FOLDER environment variable
	possibleConfs = []string{
		"/run/secrets/connected.conf",
		"/etc/connected.conf",
	}

	confs                         []WgConf
	localInIp, maskIn             string
	localOutIp, maskOut           string
	networkIdIn, networkIdOut     string
	gateway                       string
	httpPort                      string
	dnsServer, remoteIp           string
	localHostname, publicHostname string
	heartbeatIp                   = "1.1.1.1"
	nicIn                         = "eth0"
	nicOut                        string
	dnsTool                       = "/usr/bin/dnsup"
	configFilename                = "connected.conf"

	connFailed, nextPoker chan bool
	newIp                 chan string
	forwards              map[string][]*Forward
	signalChan            chan os.Signal
)

const (
	wgFolder      = "/etc/wireguard"
	configForward = "-"
)

type Forward struct {
	Host    string `json:"host"`
	Proto   string `json:"proto"`
	ExtPort string `json:"ext_port"`
	IntPort string `json:"int_port"`
	Ip      string `json:"ip"`
}

func (f *Forward) parse(s string) {
	parts := strings.Split(s, " ")
	if len(parts) == 4 {
		f.Host = configForward
		f.Proto = parts[0]
		f.ExtPort = parts[1]
		f.IntPort = parts[2]
		f.Ip = parts[3]
	} else if len(parts) == 5 {
		f.Host = parts[0]
		f.Proto = parts[1]
		f.ExtPort = parts[2]
		f.IntPort = parts[3]
		f.Ip = parts[4]
	} else {
		log.Fatalf("invalid port forward specification: %s", s)
	}
	f.add()
}

func (f *Forward) String() string {
	return fmt.Sprintf("%s: %s %s -> %s:%s", f.Host, f.Proto, f.ExtPort, f.Ip, f.IntPort)
}

func (f *Forward) add() {
	p("add forward: %s", f)
	forwards[f.Host] = append(forwards[f.Host], f)
}
func (f *Forward) remove() {
	p("remove forward: %s", f)
	var tmp []*Forward
	for _, forward := range forwards[f.Host] {
		if forward.ExtPort != f.ExtPort || forward.Proto != f.Proto {
			tmp = append(tmp, f)
		}
	}
	if len(tmp) == 0 {
		delete(forwards, f.Host)
	} else {
		forwards[f.Host] = tmp
	}
}
func (f *Forward) enable() {
	p("enable forward: %s", f)
	cmds := []string{
		fmt.Sprintf("iptables -t nat -A PREROUTING -i mullvad+ -p %s --dport %s -j DNAT --to-destination %s:%s",
			f.Proto, f.ExtPort, f.Ip, f.IntPort),
		fmt.Sprintf("iptables -t nat -A POSTROUTING -p %s -d %s --dport %s -j SNAT --to-source %s",
			f.Proto, f.Ip, f.IntPort, localInIp),
	}
	for _, cmd := range cmds {
		e := run(strings.Split(cmd, " ")...)
		chk(e)
	}
}
func (f *Forward) disable() {
	p("disable forward: %s", f)
	cmds := []string{
		fmt.Sprintf("iptables -t nat -D PREROUTING -i mullvad+ -p %s --dport %s -j DNAT --to-destination %s:%s",
			f.Proto, f.ExtPort, f.Ip, f.IntPort),
		fmt.Sprintf("iptables -t nat -D POSTROUTING -p %s -d %s --dport %s -j SNAT --to-source %s",
			f.Proto, f.Ip, f.IntPort, localInIp),
	}
	for _, cmd := range cmds {
		e := run(strings.Split(cmd, " ")...)
		chk(e)
	}
}
func loadConfig() {
	forwards = make(map[string][]*Forward)
	confFolder := os.Getenv("CONFIG_FOLDER")
	var connectedConf string
	if confFolder != "" {
		conf := filepath.Join(confFolder, configFilename)
		p("CONFIG_FOLDER environment variable set. loading conf file from %s", conf)
		b, e := os.ReadFile(conf)
		if e == nil {
			connectedConf = string(b)
		}
		if connectedConf == "" {
			p("no conf file found at %s", conf)
			os.Exit(1)
		}

	} else {
		for _, conf := range possibleConfs {
			b, e := os.ReadFile(conf)
			if e == nil {
				connectedConf = string(b)
				break
			}
		}
		if connectedConf == "" {
			p("no connected.conf file found in locations: %v", possibleConfs)
			os.Exit(1)
		}
	}
	connectedConf = strings.TrimSpace(connectedConf)

	lines := strings.Split(connectedConf, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.Split(line, "=")
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		switch k {
		case "ip_in":
			kv = strings.Split(v, "/")
			localInIp = kv[0]
			maskIn = kv[1]
			_, n, _ := net.ParseCIDR(v)
			networkIdIn = n.String()
		case "ip_out":
			kv = strings.Split(v, "/")
			localOutIp = kv[0]
			maskOut = kv[1]
			_, n, _ := net.ParseCIDR(v)
			networkIdOut = n.String()
		case "port":
			httpPort = v
		case "dns_server":
			dnsServer = v
		case "gateway":
			gateway = v
		case "nic_in":
			nicIn = v
		case "nic_out":
			nicOut = v
		case "hostname":
			publicHostname = v
		case "dns_tool":
			dnsTool = v
		case "heartbeat_ip":
			heartbeatIp = v
		case "forward":
			f := Forward{}
			f.parse(v)
		}
	}
	if nicOut == "" {
		p("no nic_out specified in config file")
		os.Exit(1)
	}
}

func copyWgConfs() {
	e := os.MkdirAll(wgFolder, 0644)
	chkFatal(e)
	confFolder := os.Getenv("CONFIG_FOLDER")
	if confFolder != "" {
		srcPath := filepath.Join(confFolder, "wireguard")
		f, e := os.ReadDir(srcPath)
		chkFatal(e)
		for _, wg := range f {
			if !wg.IsDir() && strings.HasSuffix(strings.ToLower(wg.Name()), ".conf") {
				srcFile := filepath.Join(srcPath, wg.Name())
				dstFile := filepath.Join(wgFolder, wg.Name())
				p("copying wireguard conf file: %s", srcFile)
				in, e := os.Open(srcFile)
				chkFatal(e)
				out, e := os.Create(dstFile)
				chkFatal(e)
				_, e = io.Copy(out, in)
				chkFatal(e)
				e = in.Close()
				chkFatal(e)
				e = out.Sync()
				chkFatal(e)
				e = os.Chmod(dstFile, 0440)
				chkFatal(e)
				e = out.Close()
				chkFatal(e)
			}
		}
	}
	f, e := os.ReadDir(wgFolder)
	chkFatal(e)
	if len(f) == 0 {
		p("no wireguard conf files found in /etc/wireguard")
		os.Exit(1)
	}
}

func isIp(host string) bool {
	re := regexp.MustCompile(`(\d{1,3}\.){3}\d{1,3}`)
	return re.MatchString(host)
}

type WgConf struct {
	filename string
	name     string
	endpoint string
	dns      string
}

func readWgConfs() {
	wgFiles, e := os.ReadDir(wgFolder)
	chkFatal(e)

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(wgFiles), func(i, j int) { wgFiles[i], wgFiles[j] = wgFiles[j], wgFiles[i] })

	for _, conf := range wgFiles {
		fmt.Println(conf.Name())

		if !conf.IsDir() && strings.HasSuffix(strings.ToLower(conf.Name()), ".conf") {
			outPath := filepath.Join(wgFolder, conf.Name())
			f, e := os.ReadFile(outPath)
			chkFatal(e)

			var c WgConf
			c.filename = outPath
			c.name = strings.TrimSuffix(conf.Name(), filepath.Ext(conf.Name()))

			for _, line := range strings.Split(string(f), "\n") {
				if strings.HasPrefix(line, "Endpoint") {
					line = strings.TrimPrefix(line, "Endpoint = ")
					line = strings.Split(line, ":")[0]
					c.endpoint = line
				}
				if strings.HasPrefix(line, "DNS") {
					line = strings.TrimPrefix(line, "DNS = ")
					c.dns = line
				}
			}
			confs = append(confs, c)

		}
	}
}

func makeRoutes() {
	add := func(addr string) {
		cmd := []string{"ip", "route", "add", addr, "via", gateway, "dev", nicOut}
		e := run(cmd...)
		chkFatal(e)
	}
	for _, conf := range confs {
		add(conf.endpoint)
	}
}
func deleteRoutes() {
	rm := func(addr string) {
		cmd := []string{"ip", "route", "del", addr, "via", gateway, "dev", nicOut}
		e := run(cmd...)
		chkFatal(e)
	}
	for _, conf := range confs {
		rm(conf.endpoint)
	}
}

func updateDnsHostname() {
	if publicHostname != "" {
		rsp, e := http.Get("https://ipv4.am.i.mullvad.net")
		if e != nil {
			p("failed to register dns: %s", e.Error())
			return
		}
		defer func() {
			if rsp != nil {
				e = rsp.Body.Close()
				chk(e)
			}
		}()
		if e != nil {
			connFailed <- true
			return
		}
		b, e := io.ReadAll(rsp.Body)
		remoteIp = strings.TrimSpace(string(b))
		p("VPN public IP address looks like: %s", remoteIp)
		e = run(dnsTool, publicHostname, remoteIp)
		if e != nil {
			p("failed to register dns: %s", e.Error())
			return
		}
		if newIp != nil {
			newIp <- remoteIp
		}
	}
}

func connect(conf WgConf) {
	connFailed = make(chan bool)
	nextPoker = make(chan bool)
	p("connecting to wireguard server %s at %s", conf.name, conf.endpoint)
	e := run("wg-quick", "up", conf.name)
	if e != nil {
		p("failed to connect to vpn server: %s", e.Error())
		return
	}
	p("setting up NAT and port forwarding")
	enableNat()
	setLocalDns(conf.dns)
	go updateDnsHostname()

	p("checking connection every 60 seconds")
	go func() {
		failed := 0
		for {
			time.Sleep(60 * time.Second)
			cmd := exec.Command("ping", heartbeatIp, "-c", "1")
			e = cmd.Run()
			if e != nil {
				failed++
			} else {
				failed = 0
			}
			if failed >= 3 {
				connFailed <- true
				return
			}
		}
	}()
	select {
	case <-connFailed:
		p("connection verification failed, moving to next server")
	case <-nextPoker:
		p("connection marked as failed through /next endpoint, moving to next server")
	case <-signalChan:
		p("exiting. doing cleanup.")
		p("disabling NAT")
		disableNat()
		p("deleting routes to vpn server")
		deleteRoutes()
		e = run("wg-quick", "down", conf.name)
		chk(e)
		os.Exit(0)

	}
	p("disabling NAT")
	disableNat()

	e = run("wg-quick", "down", conf.name)
	chk(e)
}
func getHostname() {
	txt, e := os.ReadFile("/etc/hostname")
	chkFatal(e)
	localHostname = strings.ReplaceAll(string(txt), "\n", "")
}

func setLocalDns(dnsServer string) {
	p("writing /etc/resolv.conf file")
	resolvConf := fmt.Sprintf("nameserver %s", dnsServer)
	e := os.WriteFile("/etc/resolv.conf", []byte(resolvConf), 0444)
	chkFatal(e)

	p("enabling DNS relay from %s to server at %s", localInIp, dnsServer)
	cmds := []string{
		fmt.Sprintf("iptables -t nat -A PREROUTING -p udp --dport 53 -j DNAT --to-destination %s:53", dnsServer),
		fmt.Sprintf("iptables -t nat -A PREROUTING -p tcp --dport 53 -j DNAT --to-destination %s:53", dnsServer),
	}
	for _, cmd := range cmds {
		e = run(strings.Split(cmd, " ")...)
		chkFatal(e)
	}
}

func enableNat() {
	cmd := fmt.Sprintf("iptables -t nat -A POSTROUTING -o mullvad+ -s %s -j MASQUERADE", networkIdIn)
	e := run(strings.Split(cmd, " ")...)
	chkFatal(e)
	for _, forward := range forwards {
		for _, f := range forward {
			f.enable()
		}
	}
}
func disableNat() {
	for _, forward := range forwards {
		for _, f := range forward {
			f.disable()
		}
	}
}
func main() {
	signalChan = make(chan os.Signal, 1)
	signal.Notify(
		signalChan,
		syscall.SIGHUP,  // kill -SIGHUP XXXX
		syscall.SIGINT,  // kill -SIGINT XXXX or Ctrl+c
		syscall.SIGQUIT, // kill -SIGQUIT XXXX
		syscall.SIGTERM, // shutdown service
	)

	loadConfig()

	copyWgConfs()
	p("connection monitor has started")

	p("writing wireguard conf files")
	readWgConfs()
	getHostname()

	p("found %d conf files", len(confs))
	for _, c := range confs {
		fmt.Printf("%+v\n", c)
	}
	p("adding routes")
	makeRoutes()

	p("making first connection")
	for {
		for _, conf := range confs {
			connect(conf)
		}
	}
}

var (
	p        = base.P
	chk      = base.Chk
	chkFatal = base.ChkFatal
	run      = base.Run
)
