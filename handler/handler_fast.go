package handler

import (
	"bytes"
	"sync"
)

// ConnectionHandlerFast impl ConnectionHandler
type ConnectionHandlerFast struct {
	Option *Option
	Sender Sender
	wg     sync.WaitGroup
}

func (h *ConnectionHandlerFast) handle(src Endpoint, dst Endpoint, c *TCPConnection) {
	key := &ConnectionKey{src: src, dst: dst}
	reqHandler := &Base{key: key, buffer: new(bytes.Buffer), option: h.Option, sender: h.Sender}
	h.wg.Add(1)
	go reqHandler.handleRequest(&h.wg, c)

	if h.Option.Resp {
		h.wg.Add(1)
		rspHandler := &Base{key: key, buffer: new(bytes.Buffer), option: h.Option, sender: h.Sender}
		go rspHandler.handleResponse(&h.wg, c)
	}
}

func (h *ConnectionHandlerFast) finish() { h.wg.Wait() }
