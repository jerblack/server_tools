package main

import (
	"encoding/json"
	"fmt"
	"github.com/jerblack/server_tools/base"
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
	forwardPath                   = "/var/lib/connected/"
	forwardFilename               = "forwards.conf"
	forwardFile                   string

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

func (f *Forward) save() {
	rule := fmt.Sprintf("%s %s %s %s %s", f.Host, f.Proto, f.ExtPort, f.IntPort, f.Ip)
	lines := []string{rule}
	if fileExists(forwardFile) {
		b, e := os.ReadFile(forwardFile)
		chkFatal(e)
		for _, line := range strings.Split(string(b), "\n") {
			if line != rule && line != "" {
				lines = append(lines, line)
			}
		}
	}
	e := os.WriteFile(forwardFile, []byte(strings.Join(lines, "\n")), 0664)
	chkFatal(e)
}
func (f *Forward) unsave() {
	rule := fmt.Sprintf("%s %s %s %s %s", f.Host, f.Proto, f.ExtPort, f.IntPort, f.Ip)
	var lines []string
	if fileExists(forwardFile) {
		b, e := os.ReadFile(forwardFile)
		chkFatal(e)
		for _, line := range strings.Split(string(b), "\n") {
			if line != rule && line != "" {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) > 0 {
		e := os.WriteFile(forwardFile, []byte(strings.Join(lines, "\n")), 0664)
		chkFatal(e)
	} else if len(lines) == 0 {
		e := os.Remove(forwardFile)
		chkFatal(e)
	}
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
		chkFatal(e)
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
		forwardFile = filepath.Join(confFolder, forwardFilename)

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
		e := os.MkdirAll(forwardPath, 0664)
		chkFatal(e)
		forwardFile = filepath.Join(forwardPath, forwardFilename)
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
		nicOut = nicIn
	}

	if fileExists(forwardFile) {
		b, e := os.ReadFile(forwardFile)
		chkFatal(e)
		for _, line := range strings.Split(string(b), "\n") {
			f := Forward{}
			f.parse(line)
		}
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

func startWeb() {
	var w Web
	w.start()
}

type Web struct{}

func (web *Web) start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/cmd", web.cmd)
	log.Fatal(http.ListenAndServe(net.JoinHostPort(localInIp, httpPort), mux))
}

func (web *Web) cmd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")
	var c Cmd
	e := json.NewDecoder(r.Body).Decode(&c)
	chk(e)
	e = r.Body.Close()
	chk(e)
	rsp := map[string]string{
		"status": "ok",
	}
	switch c.Action {
	case "ip":
		rsp["ip"] = remoteIp
		w.WriteHeader(http.StatusOK)
		e := json.NewEncoder(w).Encode(rsp)
		chk(e)
	case "next":
		for nextPoker == nil {
			time.Sleep(1 * time.Second)
		}
		newIp = make(chan string)
		ip := <-newIp
		rsp["ip"] = ip
		e := json.NewEncoder(w).Encode(rsp)
		chk(e)
	case "disable":
		var forward *Forward
		for _, f := range forwards[c.Host] {
			if f.Proto == c.Proto && f.ExtPort == c.ExtPort {
				forward = f
			}
		}
		if forward != nil {
			forward.disable()
			forward.remove()
			w.WriteHeader(http.StatusOK)
			e := json.NewEncoder(w).Encode(rsp)
			chk(e)
			forward.unsave()
		} else {
			w.WriteHeader(http.StatusNotFound)
			rsp["status"] = "no match found"
			e := json.NewEncoder(w).Encode(rsp)
			chk(e)
		}
	case "enable":
		if isAny("", c.Host, c.Ip, c.ExtPort, c.IntPort, c.Ip) {
			w.WriteHeader(http.StatusBadRequest)
			rsp["status"] = "missing required field"
			e := json.NewEncoder(w).Encode(rsp)
			chk(e)
		} else if c.Host == configForward {
			w.WriteHeader(http.StatusBadRequest)
			rsp["status"] = "invalid host"
			e := json.NewEncoder(w).Encode(rsp)
			chk(e)
		} else {
			f := Forward{
				Host:    c.Host,
				Proto:   c.Proto,
				ExtPort: c.ExtPort,
				IntPort: c.IntPort,
				Ip:      c.Ip,
			}
			f.add()
			f.enable()
			w.WriteHeader(http.StatusOK)
			e := json.NewEncoder(w).Encode(rsp)
			chk(e)
			f.save()
		}
	case "cleanup":
		var cleanup []*Forward
		for host, forward := range forwards {
			if host != configForward {
				for _, f := range forward {
					cleanup = append(cleanup, f)
				}
			}
		}
		for _, f := range cleanup {
			f.disable()
			f.remove()
			f.unsave()
		}
		w.WriteHeader(http.StatusOK)
		e := json.NewEncoder(w).Encode(rsp)
		chk(e)
	}
}

type Cmd struct {
	Action string `json:"action"`
	Forward
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

func waitForOutNic() {
	for {
		_, e := net.InterfaceByName(nicOut)
		if e == nil {
			return
		}
		p("waiting for %s to appear in container", nicOut)
		time.Sleep(3 * time.Second)
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
func updateDnsHostname() {
	if publicHostname != "" {
		rsp, e := http.Get("https://ipv4.am.i.mullvad.net")
		chk(e)
		defer func() {
			e = rsp.Body.Close()
			chk(e)
		}()
		if e != nil {
			return
		}
		b, e := io.ReadAll(rsp.Body)
		remoteIp = strings.TrimSpace(string(b))
		p("VPN public IP address looks like: %s", remoteIp)
		e = run(dnsTool, publicHostname, remoteIp)
		chkFatal(e)
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
	chk(e)
	p("setting up NAT and port forwarding")
	enableNat()
	setLocalDns(conf.dns)
	updateDnsHostname()

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
		for _, forward := range forwards {
			for _, f := range forward {
				f.disable()
			}
		}
		p("disabling NAT")
		disableNat()
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
func setStaticIp() {
	cmds := []string{
		"ip route del default",
		fmt.Sprintf("ip addr flush dev %s", nicIn),
		fmt.Sprintf("ip addr add %s/%s dev %s", localInIp, maskIn, nicIn),
		fmt.Sprintf("ip link set %s up", nicIn),
	}
	if nicIn != nicOut && localInIp != localOutIp {
		cmds = append(cmds,
			fmt.Sprintf("ip addr flush dev %s", nicOut),
			fmt.Sprintf("ip addr add %s/%s dev %s", localOutIp, maskOut, nicOut),
			fmt.Sprintf("ip link set %s up", nicOut),
		)
	}
	for _, cmd := range cmds {
		e := run(strings.Split(cmd, " ")...)
		chkFatal(e)
	}
	p("writing /etc/hosts file")
	hostFile := fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", localInIp, localHostname)
	e := os.WriteFile("/etc/hosts", []byte(hostFile), 0444)
	chkFatal(e)
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

	waitForOutNic()

	p("setting ip information")
	setStaticIp()

	p("found %d conf files", len(confs))
	for _, c := range confs {
		fmt.Printf("%+v\n", c)
	}
	p("adding routes")
	makeRoutes()

	go startWeb()
	p("making first connection")
	for {
		for _, conf := range confs {
			connect(conf)
		}
	}
}

var (
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	run        = base.Run
	isAny      = base.IsAny
	fileExists = base.FileExists
)
