package main

import (
	"context"
	"fmt"
	"github.com/jerblack/server_tools/base"
	"io"
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
		parse conf files into filename, basename, server ip

	iterate randomly through confs
		for each endpoint ip, create route through gateway
	add direct route to local gateway for split tunneled servers

	randomly select first connection
		connect
		verify connected
		if connected, update cloudflare dns record with ip obtained from https://ipv4.am.i.mullvad.net

	monitor connection
		every minute ping heartbeat ip, on 3 successive fails go to next connection

*/

var (
	// conf folder can also be specified in CONFIG_FOLDER environment variable
	possibleConfs = []string{
		"/run/secrets/connected.conf",
		"/etc/connected.conf",
	}

	wgFolder = "/etc/wireguard"

	confs                                                                           []WgConf
	ip, mask, gateway, localDns, remoteDns, remoteIp, localHostname, publicHostname string
	heartbeatIp                                                                     = "1.1.1.1"
	nic                                                                             = "eth0"
	dnsTool                                                                         = "/usr/bin/dnsup"
	splitTunnelHosts                                                                map[string][]string
	splitTunnelIps                                                                  []string
)

func loadConfig() {
	conf := os.Getenv("CONFIG_FOLDER")
	var connectedConf string
	if conf != "" {
		conf = filepath.Join(conf, "connected.conf")
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

	splitTunnelHosts = make(map[string][]string)
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
			ip = kv[0]
			mask = kv[1]
		case "remote_dns":
			remoteDns = v
		case "gateway":
			gateway = v
		case "nic":
			nic = v
		case "split_tunnel_hosts":
			if strings.Contains(v, ".") {
				for _, h := range strings.Split(v, " ") {
					if isIp(h) {
						splitTunnelIps = append(splitTunnelIps, h)
					} else {
						splitTunnelHosts[h] = []string{}
					}
				}
			}
		case "local_dns":
			localDns = v
		case "hostname":
			publicHostname = v
		case "dns_tool":
			dnsTool = v
		case "heartbeat_ip":
			heartbeatIp = v
		}
	}
	for k := range splitTunnelHosts {
		splitTunnelHosts[k] = dnsLookup(k)
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
func dnsLookup(host string) []string {
	var ips []string
	if localDns != "" {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: 5 * time.Second,
				}
				return d.DialContext(ctx, "udp", localDns+":53")
			},
		}
		ips, e := r.LookupHost(context.Background(), host)
		chkFatal(e)
		return ips
	} else {
		result, e := net.LookupIP(host)
		chkFatal(e)
		for _, _ip := range result {
			ips = append(ips, _ip.String())
		}
		return ips
	}

}

type WgConf struct {
	filename string
	name     string
	endpoint string
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
	for _, a := range splitTunnelIps {
		if isIp(a) {
			add(a)
		}
	}
	for _, ips := range splitTunnelHosts {
		for _, i := range ips {
			if isIp(i) {
				add(i)
			}
		}
	}
}
func updateDNS() {
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
	}
}
func connect(conf WgConf) {
	p("connecting to wireguard server %s at %s", conf.name, conf.endpoint)
	e := run("wg-quick", "up", conf.name)
	chk(e)
	updateDNS()
	connected := true
	p("checking connection every 60 seconds")
	failed := 0
	for connected {
		time.Sleep(60 * time.Second)
		cmd := exec.Command("ping", heartbeatIp, "-c", "1")
		e = cmd.Run()
		if e != nil {
			failed++
		} else {
			failed = 0
		}
		if failed >= 3 {
			p("connection verification failed, moving to next server")
			connected = false
		}
	}
	e = run("wg-quick", "down", conf.name)
	chk(e)
}
func getHostname() {
	txt, e := os.ReadFile("/etc/hostname")
	chkFatal(e)
	localHostname = strings.ReplaceAll(string(txt), "\n", "")
}
func fixIp() {
	cmds := []string{
		"ip route del default",
		fmt.Sprintf("ip addr flush dev %s", nic),
		fmt.Sprintf("ip addr add %s/%s dev %s", ip, mask, nic),
	}
	for _, cmd := range cmds {
		e := run(strings.Split(cmd, " ")...)
		chkFatal(e)
	}
	p("writing /etc/hosts file")
	hostFile := fmt.Sprintf("127.0.0.1 localhost\n%s %s\n", ip, localHostname)
	e := os.WriteFile("/etc/hosts", []byte(hostFile), 0444)
	chkFatal(e)

	p("writing /etc/resolv.conf file")
	resolvConf := fmt.Sprintf("nameserver %s", remoteDns)
	e = os.WriteFile("/etc/resolv.conf", []byte(resolvConf), 0444)
	chkFatal(e)
}
func main() {
	loadConfig()

	copyWgConfs()
	p("connection monitor has awakened")

	p("writing wireguard conf files")
	readWgConfs()
	getHostname()

	p("setting ip information")
	fixIp()

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
