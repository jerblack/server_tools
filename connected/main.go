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
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

/*
	at startup
		copy all embedded wg/*.conf files to /etc/wireguard

	enumerate conf files in wireguard folder
		parse conf files into filename, basename, server localIp

	iterate randomly through confs
		for each endpoint localIp, create route through gateway
	add direct route to local gateway for split tunneled servers

	randomly select first connection
		connect
		verify connected
		if connected, update cloudflare dns record with localIp obtained from https://ipv4.am.i.mullvad.net

	monitor connection
		every minute ping heartbeat localIp, on 3 successive fails go to next connection

*/

var (
	// conf folder can also be specified in CONFIG_FOLDER environment variable
	possibleConfs = []string{
		"/run/secrets/connected.conf",
		"/etc/connected.conf",
	}

	wgFolder = "/etc/wireguard"

	confs                         []WgConf
	localIp, mask, gateway        string
	networkId                     string
	httpPort                      string
	dnsServer, remoteIp           string
	localHostname, publicHostname string
	heartbeatIp                   = "1.1.1.1"
	nic                           = "eth0"
	dnsTool                       = "/usr/bin/dnsup"

	connFailed, nextPoker chan bool
	newIp                 chan string
	forwards              []*Forward
)

type Forward struct {
	Proto, IntPort, ExtPort, Ip string
}

func (f *Forward) parse(s string) {
	parts := strings.Split(s, " ")
	if len(parts) != 4 {
		log.Fatalf("invalid port forward specification: %s", s)
	}
	f.Proto = parts[0]
	f.ExtPort = parts[1]
	f.IntPort = parts[2]
	f.Ip = parts[3]
	forwards = append(forwards, f)
}

func loadConfig() {
	confFolder := os.Getenv("CONFIG_FOLDER")
	var connectedConf string
	if confFolder != "" {
		conf := filepath.Join(confFolder, "connected.conf")
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
		case "ip":
			kv = strings.Split(v, "/")
			localIp = kv[0]
			mask = kv[1]
			_, n, _ := net.ParseCIDR(v)
			networkId = n.String()
		case "port":
			httpPort = v
		case "dns_server":
			dnsServer = v
		case "gateway":
			gateway = v
		case "nic":
			nic = v
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
	mux.HandleFunc("/ip", web.getIp)
	mux.HandleFunc("/next", web.next)
	log.Fatal(http.ListenAndServe(net.JoinHostPort(localIp, httpPort), mux))
}
func (web *Web) getIp(w http.ResponseWriter, r *http.Request) {
	rsp := map[string]string{
		"ip": remoteIp,
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	e := json.NewEncoder(w).Encode(rsp)
	chk(e)
}
func (web *Web) next(w http.ResponseWriter, r *http.Request) {
	for nextPoker == nil {
		time.Sleep(1 * time.Second)
	}
	newIp = make(chan string)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	nextPoker <- true
	_ip := <-newIp
	rsp := map[string]string{
		"ip": _ip,
	}
	e := json.NewEncoder(w).Encode(rsp)
	chk(e)
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
		cmd := []string{"ip", "route", "add", addr, "via", gateway, "dev", nic}
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
		fmt.Sprintf("ip addr flush dev %s", nic),
		fmt.Sprintf("ip addr add %s/%s dev %s", localIp, mask, nic),
	}
	for _, cmd := range cmds {
		e := run(strings.Split(cmd, " ")...)
		chkFatal(e)
	}
	p("writing /etc/hosts file")
	hostFile := fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", localIp, localHostname)
	e := os.WriteFile("/etc/hosts", []byte(hostFile), 0444)
	chkFatal(e)
}
func setLocalDns(dnsServer string) {
	p("writing /etc/resolv.conf file")
	resolvConf := fmt.Sprintf("nameserver %s", dnsServer)
	e := os.WriteFile("/etc/resolv.conf", []byte(resolvConf), 0444)
	chkFatal(e)

	p("enabling DNS relay from %s to server at %s", localIp, dnsServer)
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
	cmds := []string{
		fmt.Sprintf("iptables -t nat -A POSTROUTING -o mullvad+ -s %s -j MASQUERADE", networkId),
	}
	for _, f := range forwards {
		pre := fmt.Sprintf("iptables -t nat -A PREROUTING -i mullvad+ -p %s --dport %s -j DNAT --to-destination %s:%s",
			f.Proto, f.ExtPort, f.Ip, f.IntPort)
		post := fmt.Sprintf("iptables -t nat -A POSTROUTING -p %s -d %s --dport %s -j SNAT --to-source %s",
			f.Proto, f.Ip, f.IntPort, localIp)
		cmds = append(cmds, pre, post)
	}
	for _, cmd := range cmds {
		e := run(strings.Split(cmd, " ")...)
		chkFatal(e)
	}
}
func disableNat() {
	e := run("iptables", "-t", "nat", "-F")
	chkFatal(e)
}
func main() {
	loadConfig()

	copyWgConfs()
	p("connection monitor has started")

	p("writing wireguard conf files")
	readWgConfs()
	getHostname()

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
	p        = base.P
	chk      = base.Chk
	chkFatal = base.ChkFatal
	run      = base.Run
)
