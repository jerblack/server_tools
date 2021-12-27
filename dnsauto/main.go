package main

import (
	"encoding/json"
	"fmt"
	"github.com/jerblack/base"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

/*
automatically check public ip of connection and update registration with cloudflare dns when IP changes

when run
	- check current public ip with https://ipv4.am.i.mullvad.net (or configured service)
    - check ip from last check stored in cache file
    - if they don't match
		- update configured hostname with new ip cloudflare
        - save new ip to cache file

config file options
	token -> required, cloudflare token with dns:edit:zone permission
	hostname -> required, dns name to update with new ip on cloudflare
    ip_checker -> optional, url for service that returns public ip (in same bare format as the default provider, ip only, no json)
*/

const (
	cacheFile    = "/var/cache/dnsauto/last_ip"
	domainIdFile = "/var/cache/dnsauto/domain_id"
	hostIdFile   = "/var/cache/dnsauto/host_id"
	configFile   = "/etc/dnsauto"
)

var (
	domainId, hostId  string
	token, hostName   string
	ipCheckUrl        = "https://ipv4.am.i.mullvad.net"
	lastIp, currentIp string
)

func main() {
	verifyCacheFolder()
	loadConfig()
	getLastIp()
	getCurrentIp()
	if lastIp != currentIp {
		getDomainId()
		getHostId()
		updateDNS()
		cacheCurrentIp()
	}

}

func loadConfig() {
	if !fileExists(configFile) {
		p("config file not found at %s", configFile)
		os.Exit(1)
	}

	b, e := os.ReadFile(configFile)
	chkFatal(e, "could not read config file")
	conf := string(b)
	conf = strings.TrimSpace(conf)
	lines := strings.Split(conf, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.Split(line, "=")
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		switch k {
		case "token":
			token = v
		case "hostname":
			hostName = v
		case "ip_checker":
			ipCheckUrl = v
		}
	}
	if token == "" {
		p("no token in config file")
		os.Exit(1)
	}
	if hostName == "" {
		p("no hostname in config file")
		os.Exit(1)
	}
}

func getLastIp() {
	if !fileExists(cacheFile) {
		p("no previous ip found in cache file. assuming first run.")
		return
	}
	b, e := os.ReadFile(cacheFile)
	chkFatal(e, "could not read cache file")
	lastIp = string(b)
	p("last public IP: %s", lastIp)
}
func isIp(host string) bool {
	re := regexp.MustCompile(`(\d{1,3}\.){3}\d{1,3}`)
	return re.MatchString(host)
}
func getCurrentIp() {
	rsp, e := http.Get(ipCheckUrl)
	chkFatal(e, "error checking current ip")
	defer func() {
		if rsp != nil {
			e = rsp.Body.Close()
			chk(e)
		}
	}()

	b, e := io.ReadAll(rsp.Body)
	currentIp = strings.TrimSpace(string(b))
	if !isIp(currentIp) {
		p("did not receive valid ip from checker: %s", currentIp)
		os.Exit(1)
	}
	p("current public IP: %s", currentIp)
}
func verifyCacheFolder() {
	folder := filepath.Dir(cacheFile)
	e := os.MkdirAll(folder, 0755)
	chkFatal(e, "could not create folder for cache file")
}
func cacheCurrentIp() {
	e := os.WriteFile(cacheFile, []byte(currentIp), 0664)
	chkFatal(e, "could not write cache file")
}

type CfResults struct {
	Result  []CfResult `json:"result"`
	Success bool       `json:"success"`
}
type CfResult struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

func setHeaders(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")
}

func getDomainId() {
	domainId = getFile(domainIdFile)
	if domainId != "" {
		return
	}
	domainName := strings.ToLower(strings.SplitN(hostName, ".", 2)[1])
	uri := "https://api.cloudflare.com/client/v4/zones/"
	client := &http.Client{}
	req, _ := http.NewRequest("GET", uri, nil)
	setHeaders(req)
	rsp, e := client.Do(req)
	chk(e)
	defer func() {
		e = rsp.Body.Close()
		chk(e)
	}()
	rspData, e := ioutil.ReadAll(rsp.Body)
	var results CfResults
	e = json.Unmarshal(rspData, &results)
	chkFatal(e)
	for _, result := range results.Result {
		if strings.ToLower(result.Name) == domainName {
			domainId = result.Id
			break
		}
	}
	if domainId == "" {
		log.Fatalf("COULD NOT FIND CLOUDFLARE ZONE ID FOR DOMAIN: %s\n", domainName)
	}
	setFile(domainIdFile, domainId)
}
func getHostId() {
	hostId = getFile(hostIdFile)
	if hostId != "" {
		return
	}
	uri := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", domainId)
	client := &http.Client{}
	req, _ := http.NewRequest("GET", uri, nil)
	setHeaders(req)
	rsp, e := client.Do(req)
	chk(e)
	defer func() {
		e = rsp.Body.Close()
		chk(e)
	}()
	rspData, e := ioutil.ReadAll(rsp.Body)
	var results CfResults
	e = json.Unmarshal(rspData, &results)
	chkFatal(e)
	for _, result := range results.Result {
		if strings.ToLower(result.Name) == hostName {
			hostId = result.Id
			break
		}
	}
	if hostId == "" {
		log.Fatalf("COULD NOT FIND CLOUDFLARE HOST ID FOR HOSTNAME: %s\n", hostName)
	}
	setFile(hostIdFile, hostId)
}
func updateDNS() {
	uri := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", domainId, hostId)
	data := fmt.Sprintf(`{"type":"A","name":"%s","content":"%s","ttl":120,"proxied":false}`, hostName, currentIp)
	client := &http.Client{}
	req, _ := http.NewRequest("PUT", uri, strings.NewReader(data))
	setHeaders(req)
	rsp, e := client.Do(req)
	chk(e)
	defer func() {
		e = rsp.Body.Close()
		chk(e)
	}()
	rspData, e := ioutil.ReadAll(rsp.Body)
	rspText := string(rspData)
	if strings.Contains(rspText, `"success":true`) {
		p("successfully updated dns hostname %s with new ip %s on cloudflare", hostName, currentIp)
	} else {
		p("hostname update failed")
		p(rspText)
	}
}

var (
	fileExists = base.FileExists
	p          = base.P
	chk        = base.Chk
	chkFatal   = base.ChkFatal
	getFile    = base.GetFile
	setFile    = base.SetFile
)
