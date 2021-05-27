package main

import (
	"fmt"
	. "github.com/jerblack/server_tools/base"
	"os"
)

const (
	kodiIp = "192.168.0.226:9090"
)

var (
	notificationMethods = []string{
		"Player.OnAVChange",
		"Player.OnAVStart",
		"Player.OnPause",
		"Player.OnPlay",
		"Player.OnPropertyChanged",
		"Player.OnResume",
		"Player.OnSeek",
		"Player.OnSpeedChanged",
		"Player.OnStop",
	}
)

func NewKodi() *Kodi {
	k := Kodi{}
	c, e := NewClient(kodiIp, nil)
	if e != nil {
		fmt.Println(e)
		os.Exit(1)
	}
	k.client = c
	return &k
}

type Kodi struct {
	client *Client
}

func (k *Kodi) _action(action string) {
	method := `Input.ExecuteAction`
	params := map[string]string{"action": action}
	rsp, e := k.client.Call(method, params)
	if e != nil {
		fmt.Println(e)
	}
	fmt.Println(rsp)
}
func (k *Kodi) playpause()   { k._action("playpause") }
func (k *Kodi) play()        { k._action("play") }
func (k *Kodi) pause()       { k._action("pause") }
func (k *Kodi) back()        { k._action("back") }
func (k *Kodi) info()        { k._action("info") }
func (k *Kodi) menu()        { k._action("menu") }
func (k *Kodi) rightclick()  { k._action("rightclick") }
func (k *Kodi) contextmenu() { k._action("contextmenu") }
func (k *Kodi) enter()       { k._action("enter") }
func (k *Kodi) up()          { k._action("up") }
func (k *Kodi) down()        { k._action("down") }
func (k *Kodi) left()        { k._action("left") }
func (k *Kodi) right()       { k._action("right") }
func (k *Kodi) selectBtn()   { k._action("select") }
func (k *Kodi) stop()        { k._action("stop") }
func (k *Kodi) _method(method string, params interface{}) {
	rsp, e := k.client.Call(method, params)
	if e != nil {
		fmt.Println(e)
	}
	fmt.Println(rsp)
}
func (k *Kodi) home() { k._method("Input.Home", nil) }
func (k *Kodi) seekTime(seconds int) {
	var hours, minutes int
	if seconds > 59 {
		minutes = seconds / 60
		seconds = seconds % 60
	}
	if minutes > 59 {
		hours = minutes / 60
		minutes = minutes % 60
	}

	k._method("Player.Seek", map[string]interface{}{
		"playerid": 1,
		"value": map[string]interface{}{
			"time": map[string]int{
				"seconds": seconds,
				"minutes": minutes,
				"hours":   hours,
			},
		},
	})
}
func (k *Kodi) seekPercent(percent float32) {
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	k._method("Player.Seek", map[string]interface{}{
		"playerid": 1,
		"value": map[string]float32{
			"percentage": percent,
		},
	})
}

func main() {
	w := Web{}
	w.start()
	select {}
}

var (
	p              = P
	chk            = Chk
	chkFatal       = ChkFatal
	containsString = ContainsString
	run            = Run
	rmEmptyFolders = RmEmptyFolders
	isDirEmpty     = IsDirEmpty
	getAltPath     = GetAltPath
	isAny          = IsAny
	fileExists     = FileExists
)
