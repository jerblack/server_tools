package main

import (
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

type CmdResult struct {
	Greetings string                 `json:"greetings human"`
	Cmd       string                 `json:"cmd"`
	Args      map[string]interface{} `json:"args"`
	Success   bool                   `json:"success"`
	Output    string                 `json:"output"`
}

func haCmd(verb string) {
	uri := haUri + verb
	rsp, e := http.Get(uri)
	chk(e)
	rsp.Body.Close()
}

func startWeb() {
	w := Web{}
	w.start()
}

type Web struct {
	lastEvent map[string]interface{}
}

func (web *Web) start() {
	p("kodictl web listening on %s", uri)
	//go web.notificationRelay()
	mux := http.NewServeMux()
	mux.HandleFunc("/", web.root)
	mux.HandleFunc("/cmd", web.cmd)
	mux.Handle("/events", web.events())
	//log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%s", listenIp, listenPort), mux))
	go func() {
		s80 := &http.Server{
			Addr:    ":80",
			Handler: mux,
		}
		log.Fatal(s80.ListenAndServe())
	}()
	go func() {
		pem, _ := content.ReadFile("web/kodictl.pem")
		crt, _ := content.ReadFile("web/kodictl.crt")
		key, _ := content.ReadFile("web/kodictl.key")

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

func (web *Web) handleCmd(cmd string, args map[string]interface{}) (result string, e error) {
	switch cmd {
	case "play_pause":
		kodi.playPause()
	case "play":
		kodi.play()
	case "pause":
		kodi.pause()
	case "back":
		kodi.back()
	case "info":
		kodi.info()
	case "menu":
		kodi.menu()
	case "right_click":
		kodi.rightClick()
	case "context_menu":
		kodi.contextMenu()
	case "up":
		kodi.up()
	case "down":
		kodi.down()
	case "left":
		kodi.left()
	case "right":
		kodi.right()
	case "select":
		kodi.selectBtn()
	case "stop":
		kodi.stop()
	case "home":
		kodi.home()
	case "next":
		kodi.next()
	case "prev":
		kodi.prev()
	case "cat_videos":
		haCmd("catvideos")
	case "kodi_open":
		haCmd("openkodi")
	case "kodi_close":
		haCmd("closekodi")
	case "tv_on":
		haCmd("tvon")
	case "tv_off":
		haCmd("tvoff")
	case "mute":
		haCmd("mute")
	case "delete":
		kodi.delete()

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
			kodi.seekTime(int(sec.(float64)))
		case "percent":
			pct, ok := args["percent"]
			if !ok {
				e = fmt.Errorf("no percent parameter specified with seek type percent")
				return
			}
			kodi.seekPercent(pct.(float64))
		}
	case "get_playlists":
		b, _ := json.Marshal(playlists)
		result = string(b)

	case "start_playlist":
		nameHeader, ok := args["name"]
		if !ok {
			e = fmt.Errorf("no name parameter specified with start_playlist command")
			return
		}
		name := strings.ToLower(nameHeader.(string))
		pl, ok := playlists[name]
		if !ok {
			e = fmt.Errorf("invalid name specified with start_playlist command : %s", name)
			return
		}
		kodi.playPlaylist(pl)
	case "now_playing":
		b, _ := json.Marshal(np)
		result = string(b)
	case "title":
		m, e := kodi.getPlayerItem()
		chk(e)
		b, _ := json.Marshal(m)
		result = string(b)
	case "paused":
		m, e := kodi.getPlayerProperties()
		chk(e)
		b, _ := json.Marshal(m)
		result = string(b)
	}
	return
}

//go:embed web/*
var content embed.FS

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
			//thumb.numClients <- len(receiverMap)
		}()
		//if web.lastEvent != nil {
		//	notify.pushUpdate("kodi_event", web.lastEvent)
		//}
		isDone := r.Context().Done()
		flusher := w.(http.Flusher)
		//thumb.numClients <- len(receiverMap)
		//position.push()
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
func (web *Web) events() http.HandlerFunc {
	// clients connect here to receive event stream updates
	fn := func(w http.ResponseWriter, r *http.Request) {
		cmds := make(chan string, 128)
		web.makeEventStream(cmds, notify.clients)(w, r)
	}
	return fn
}
