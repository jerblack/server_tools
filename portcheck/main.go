package main

import (
	"encoding/json"
	"fmt"
	"github.com/jerblack/server_tools/base"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	// conf folder can also be specified in CONFIG_FOLDER environment variable
	possibleConfs = []string{
		"/run/secrets/portcheck.conf",
		"/etc/portcheck.conf",
	}
	timeout                = 3 * time.Second
	interval               = 10 * time.Minute
	startDelay             = 10 * time.Second
	intIp, extIp, httpPort string
	ports                  []string
)

func loadConfig() {
	confPath := os.Getenv("CONFIG_FOLDER")
	var conf string
	if confPath != "" {
		confPath = filepath.Join(confPath, "portcheck.conf")
		p("CONFIG_FOLDER environment variable set. loading conf file from %s", conf)
		b, e := os.ReadFile(confPath)
		if e == nil {
			conf = string(b)
		}
		if conf == "" {
			p("no conf file found at %s", confPath)
			os.Exit(1)
		}

	} else {
		for _, c := range possibleConfs {
			b, e := os.ReadFile(c)
			if e == nil {
				conf = string(b)
				break
			}
		}
		if conf == "" {
			p("no portcheck.conf file found in locations: %v", possibleConfs)
			os.Exit(1)
		}
	}
	conf = strings.TrimSpace(conf)
	lines := strings.Split(conf, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.Split(line, "=")
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		switch k {
		case "http_ip":
			intIp = v
		case "http_port":
			httpPort = v
		case "ports":
			ports = append(ports, strings.Split(v, " ")...)
		}
	}
}

type VPN struct {
	Ip string `json:"ip"`
}

func (v *VPN) get(endpoint string) {
	rsp, e := http.Get(fmt.Sprintf("http://%s:%s/%s", intIp, httpPort, endpoint))
	chk(e)
	if chk == nil {
		defer rsp.Body.Close()
		e = json.NewDecoder(rsp.Body).Decode(v)
		chk(e)
	}

}
func (v *VPN) First() {
	v.get("ip")
}

func (v *VPN) Next() {
	v.get("next")
}

func checker() {

	var vpn VPN
	vpn.First()
	if vpn.Ip == "" {
		p("no ip retrieved from /ip endpoint on connected")
		return
	}
	for {
		nextCalled := false

		for _, port := range ports {
			if nextCalled {
				continue
			}
			conn, e := net.DialTimeout("tcp", net.JoinHostPort(vpn.Ip, port), timeout)
			chk(e)
			if e != nil || conn == nil {
				p("port %s not available on remote ip %s. checking port on local ip %s", port, vpn.Ip, intIp)
				conn, e = net.DialTimeout("tcp", net.JoinHostPort(intIp, port), timeout)
				chk(e)
				if e != nil || conn == nil {
					p("port %s not available on local ip %s. service is down.", port, intIp)
				} else {
					p("port %s accessible internally. port forward not working.")
					p("telling connected to move to next server")
					lastIp := vpn.Ip
					vpn.Next()
					nextCalled = true
					if vpn.Ip == lastIp {
						p("Remote IP did not change. Connected did not change servers")
						return
					}
					if vpn.Ip == "" {
						p("No remote IP returned from Connected.")
						return
					}
				}
			} else {
				e = conn.Close()
				chk(e)
			}
		}
		time.Sleep(interval)
	}
}

func main() {
	loadConfig()
	time.Sleep(startDelay)
	for {
		p("starting checker")
		checker()
		p("checker failed. restarting checker in 1 minute")
		time.Sleep(1 * time.Minute)
	}
}

var (
	p        = base.P
	chk      = base.Chk
	chkFatal = base.ChkFatal
	getFile  = base.GetFile
	setFile  = base.SetFile
)
