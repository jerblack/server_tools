package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/jerblack/server_tools/base"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
)

/*
	update ip for hostname on cloudflare hosted dns
	dnsup <hostname> <ip>
	dnsup host.domain.com 1.2.3.4

	cloudflare api key and cloudflare mail will be read from file named dnsup.conf in either /run/secrets/ or /etc/
		example dnsup.conf:
			mail=
			apikey=
		In practice, dnsup.conf file will be injected using secrets defined in the docker-compose,
		Dockerfile will look for conf file in .secrets/dnsup.conf


*/

var (
	possibleConfs = []string{
		"/run/secrets/dnsup.conf",
		"/etc/dnsup.conf",
	}

	apiKey, apiMail  string
	hostname, ip     string
	domainId, hostId string
	domainIdFile     = "/tmp/domain_id"
	hostIdFile       = "/tmp/host_id"
)

func loadConfig() {
	var confFile string

	for _, conf := range possibleConfs {
		b, e := os.ReadFile(conf)
		if e == nil {
			confFile = string(b)
			break
		}
	}
	if confFile == "" {
		p("no dnsup.conf file found in locations: %v", possibleConfs)
		os.Exit(1)
	}

	confFile = strings.TrimSpace(confFile)

	lines := strings.Split(confFile, "\n")
	reEq := regexp.MustCompile(`\s*=\s*`)
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

		switch k {
		case "mail":
			apiMail = v
		case "apikey":
			apiKey = v
		}
	}

	if apiKey == "" && apiMail == "" {
		log.Fatal("CLOUDFLARE API KEY AND CLOUDFLARE MAIL NOT FOUND")
	} else if apiKey == "" {
		log.Fatal("CLOUDFLARE API KEY NOT FOUND")
	} else if apiMail == "" {
		log.Fatal("CLOUDFLARE MAIL NOT FOUND")
	}
}

func setHeaders(req *http.Request) {
	req.Header.Set("X-Auth-Email", apiMail)
	req.Header.Set("X-Auth-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
}

type CfResults struct {
	Result  []CfResult `json:"result"`
	Success bool       `json:"success"`
}
type CfResult struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

func getDomainId() {
	domainId = getFile(domainIdFile)
	if domainId != "" {
		return
	}
	domainName := strings.ToLower(strings.SplitN(hostname, ".", 2)[1])
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
		if strings.ToLower(result.Name) == hostname {
			hostId = result.Id
			break
		}
	}
	if hostId == "" {
		log.Fatalf("COULD NOT FIND CLOUDFLARE HOST ID FOR HOSTNAME: %s\n", hostname)
	}
	setFile(hostIdFile, hostId)
}
func updateDNS() {
	uri := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", domainId, hostId)
	data := fmt.Sprintf(`{"type":"A","name":"%s","content":"%s","ttl":120,"proxied":false}`, hostname, ip)
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
		p("successfully updated dns hostname %s with new ip %s on cloudflare", hostname, ip)
	} else {
		p("hostname update failed")
		p(rspText)
	}
}

func getArgs() {
	help := func() {
		fmt.Println("dnsup <hostname> <ip>\n" +
			"dnsup home.domain.com 1.2.3.4\n\n" +
			"requires:\n" +
			"cloudflare api key: as docker secret cf_api or environment variable CF_API\n" +
			"cloudflare mail: as docker secret cf_mail or environment variable CF_MAIL\n" +
			"specify docker secrets in docker-compose.yml if not using swarm.")
		os.Exit(1)
	}
	if len(os.Args) != 3 {
		help()
	}
	hostname = os.Args[1]
	ip = os.Args[2]
}

func main() {
	getArgs()
	loadConfig()
	getDomainId()
	getHostId()
	updateDNS()
}

var (
	p        = base.P
	chk      = base.Chk
	chkFatal = base.ChkFatal
	getFile  = base.GetFile
	setFile  = base.SetFile
)
