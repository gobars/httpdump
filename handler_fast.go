package main

import (
	"bytes"
	"sync"
)

// ConnectionHandlerFast impl ConnectionHandler
type ConnectionHandlerFast struct {
	option *Option
	sender Sender
	wg     sync.WaitGroup
}

func (h *ConnectionHandlerFast) handle(src Endpoint, dst Endpoint, c *TCPConnection) {
	key := &ConnectionKey{src: src, dst: dst}
	reqHandler := &HandlerBase{key: key, buffer: new(bytes.Buffer), option: h.option, sender: h.sender}
	h.wg.Add(1)
	go reqHandler.handleRequest(&h.wg, c)

	if h.option.Resp {
		h.wg.Add(1)
		rspHandler := &HandlerBase{key: key, buffer: new(bytes.Buffer), option: h.option, sender: h.sender}
		go rspHandler.handleResponse(&h.wg, c)
	}
}

func (h *ConnectionHandlerFast) finish() { h.wg.Wait() }
