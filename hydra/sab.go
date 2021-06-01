package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type SabHistory struct {
	History struct {
		Slots []struct {
			Id         int    `json:"id"`
			Completed  int64  `json:"completed"`
			Name       string `json:"name"`
			NzbName    string `json:"nzb_name"`
			Status     string `json:"status"`
			NzoId      string `json:"nzo_id"`
			Storage    string `json:"storage"`
			Path       string `json:"path"`
			ScriptLine string `json:"script_line"`
		} `json:"slots"`
	} `json:"history"`
}

func (s *SabHistory) get() {
	uri := fmt.Sprintf("http://%s:%s/sabnzbd/api/?output=json&apikey=%s&mode=history", sabIp, sabPort, sabKey)
	rsp, e := http.Get(uri)
	chk(e)
	defer func(r *http.Response) {
		e := r.Body.Close()
		chk(e)
	}(rsp)

	rspData, e := ioutil.ReadAll(rsp.Body)
	e = json.Unmarshal(rspData, s)
	chk(e)

}
