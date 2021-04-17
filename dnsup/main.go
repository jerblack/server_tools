package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

/*
	update ip for hostname on cloudflare hosted dns
	dnsup <hostname> <ip>
	dnsup host.domain.com 1.2.3.4

	cloudflare api key and cloudflare mail will be read from file named dnsup.conf in same directory as main.go at compilation stage
		example dnsup.conf:
			mail=
			apikey=
		In practice, dnsup.conf file will be in secrets docker image, copied into the go layer during the build process
		and compiled directly into the final dnsup binary, which is what will be copied into the final image.
			Dockerfile will look for dnsup.conf file at root of secrets image


*/

var (
	//go:embed dnsup.conf
	confFile string

	apiKey, apiMail  string
	hostname, ip     string
	domainId, hostId string
	domainIdFile     = "/tmp/domain_id"
	hostIdFile       = "/tmp/host_id"
)

func loadConfig() {
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
	defer rsp.Body.Close()
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
	defer rsp.Body.Close()
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
	defer rsp.Body.Close()
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

func p(s string, i ...interface{}) {
	now := time.Now()
	t := strings.ToLower(strings.TrimRight(now.Format("3.04PM"), "M"))
	notice := fmt.Sprintf("%s | %s", t, fmt.Sprintf(s, i...))
	fmt.Println(notice)
}
func chkFatal(err error) {
	if err != nil {
		fmt.Println("----------------------")
		panic(err)
	}
}
func chk(err error) {
	if err != nil {
		fmt.Println("----------------------")
		fmt.Println(err)
		fmt.Println("----------------------")
	}
}
func getFile(file string) string {
	b, e := os.ReadFile(file)
	if e == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}
func setFile(file, val string) {
	_ = os.WriteFile(file, []byte(val), 0400)
}
