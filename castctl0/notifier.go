package main

import (
	"fmt"
)

type NotifyEvent struct {
	Event string
	Data  string
}

func (ev *NotifyEvent) format() string {
	return fmt.Sprintf("event:%s\ndata: %s\n\n", ev.Event, ev.Data)
}

type Notify struct {
	clients map[chan string]string
}

func NewNotify() *Notify {
	return &Notify{
		clients: make(map[chan string]string),
	}
}
func (n *Notify) pushUpdate(event, data string) {
	ev := NotifyEvent{
		Event: event, Data: data,
	}
	e := ev.format()
	for client, _ := range n.clients {
		client <- e
	}
}
