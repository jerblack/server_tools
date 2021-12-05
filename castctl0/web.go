package main

import (
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/jerblack/server_tools/base"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed web/*
var content embed.FS

type CmdResult struct {
	Greetings string                 `json:"greetings human"`
	Cmd       string                 `json:"cmd"`
	Args      map[string]interface{} `json:"args"`
	Success   bool                   `json:"success"`
	Output    string                 `json:"output"`
}

func startWeb() {
	w := Web{}
	w.start()
}

type Web struct{}

func (web *Web) start() {
	p("castctl web listening on http(s)://castctl")
	mux := http.NewServeMux()
	mux.HandleFunc("/", web.root)
	mux.HandleFunc("/cmd", web.cmd)
	mux.HandleFunc("/events", web.events)
	go func() {
		s80 := &http.Server{
			Addr:    ":80",
			Handler: mux,
		}
		log.Fatal(s80.ListenAndServe())
	}()
	go func() {
		pem, _ := content.ReadFile("web/castctl.pem")
		crt, _ := content.ReadFile("web/castctl.crt")
		key, _ := content.ReadFile("web/castctl.key")

		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(pem)

		cert, _ := tls.X509KeyPair(append(crt, pem...), key)
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      certPool,
		}

		s443 := &http.Server{
			Addr:      ":443",
			Handler:   mux,
			TLSConfig: tlsConfig,
		}
		log.Fatal(s443.ListenAndServeTLS("", ""))
	}()
	select {}
}

func (web *Web) cmd(w http.ResponseWriter, r *http.Request) {
	args := make(map[string]interface{})
	cmd := r.Header.Get("cmd")
	argHeader := r.Header.Get("args")
	_ = json.Unmarshal([]byte(argHeader), &args)
	fmt.Printf("cmd: %s\n", cmd)
	if argHeader != "undefined" && argHeader != "{}" {
		fmt.Printf("arg: %s\n", argHeader)
	}
	result, err := web.handleCmd(cmd, args)
	response := CmdResult{
		Greetings: "hello",
		Cmd:       cmd,
		Args:      args,
		Success:   err == nil,
		Output:    result,
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	e := json.NewEncoder(w).Encode(response)
	chkFatal(e)
}
func (web *Web) root(w http.ResponseWriter, r *http.Request) {
	// if url http://www.web.com/file.png | r.URL.path -> /file.png
	fmt.Println(r.URL.Path)
	uri := r.URL.Path
	var fName string
	switch {
	case uri == "/":
		fName = "web/web.html"
	default:
		fName = filepath.Join("web", uri)
	}
	ext := filepath.Ext(fName)
	mimeType := mime.TypeByExtension(ext)
	w.Header().Set("Content-Type", mimeType)

	b, e := content.ReadFile(fName)

	if e != nil {
		fmt.Println(e)
		http.Redirect(w, r, "/", 301)
	} else {
		_, e = w.Write(b)
		chk(e)
	}
}
func (web *Web) makeEventStream(cmds chan string, receiverMap map[chan string]string) http.HandlerFunc {
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Cache-Control,Connection")
		fmt.Println(r.URL.Path)
		clientIp := strings.Split(r.RemoteAddr, ":")[0]
		fmt.Printf("%s has connected\n", clientIp)
		receiverMap[cmds] = clientIp
		defer func() {
			close(cmds)
			delete(receiverMap, cmds)
			chk(r.Body.Close())
		}()

		isDone := r.Context().Done()
		flusher := w.(http.Flusher)

		for {
			select {
			case <-isDone:
				return
			case cmd := <-cmds:
				if cmd != "" {
					_, err := fmt.Fprintf(w, cmd)
					chk(err)
					flusher.Flush()
				}
			}
		}
	}
	return fn
}
func (web *Web) events(w http.ResponseWriter, r *http.Request) {
	// clients connect here to receive event stream updates
	cmds := make(chan string, 128)
	web.makeEventStream(cmds, notify.clients)(w, r)

}

func (web *Web) handleCmd(cmd string, args map[string]interface{}) (result string, e error) {
	switch cmd {
	case "play_pause":
		cast.playPause()
	case "play":
		cast.play()
	case "pause":
		cast.pause()
	case "stop":
		cast.stop()
	case "next":
		cast.next()
	case "prev":
		cast.prev()
	case "cat_videos":
		haCmd("catvideos")
	case "tv_on":
		haCmd("tvon")
	case "tv_off":
		haCmd("tvoff")
	case "tv_power":
		haCmd("tvpower")
	case "mute":
		haCmd("mute")
	case "delete":
		deleteCurrent()
	case "seek":
		seekType, ok := args["type"]
		if !ok {
			e = fmt.Errorf("no type specified with seek")
			return
		}
		switch seekType.(string) {
		case "second":
			sec, ok := args["second"]
			if !ok {
				e = fmt.Errorf("no second parameter specified with seek type second")
				return
			}
			cast.seek(sec.(float64))
		case "percent":
			pct, ok := args["percent"]
			if !ok {
				e = fmt.Errorf("no percent parameter specified with seek type percent")
				return
			}
			cast.seekPercent(pct.(float64))
		}
	case "get_playlists":
		b, _ := json.Marshal(playlists)
		result = string(b)
	case "start_playlist":
		idHeader, ok := args["id"]
		if !ok {
			e = fmt.Errorf("no name parameter specified with start_playlist command")
			return
		}
		id := strings.ToLower(idHeader.(string))
		pl, ok := playlists[id]
		if !ok {
			e = fmt.Errorf("invalid id specified with start_playlist command : %s", id)
			return
		}
		go cast.playPlaylist(pl)
	case "now_playing":
		b, _ := json.Marshal(cast.np)
		result = string(b)
	}
	return
}

func haCmd(verb string) {
	uri := haUri + verb
	rsp, e := http.Get(uri)
	chk(e)
	rsp.Body.Close()
}

var (
	getLocalIp = base.GetLocalIp()
)
