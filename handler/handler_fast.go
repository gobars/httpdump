package handler

import (
	"context"
	"sync"
)

// ConnectionHandlerFast impl ConnectionHandler
type ConnectionHandlerFast struct {
	context.Context
	Option *Option
	Sender Sender
	wg     sync.WaitGroup
}

func (h *ConnectionHandlerFast) handle(src Endpoint, dst Endpoint, c *TCPConnection) {
	key := &ConnectionKey{src: src, dst: dst}
	b := &Base{Context: h.Context, key: key, option: h.Option, sender: h.Sender, usingJSON: IsUsingJSON()}

	h.wg.Add(1)
	go b.handleRequest(&h.wg, c)

	if h.Option.Resp {
		h.wg.Add(1)
		rspHandler := &Base{Context: h.Context, key: key, option: h.Option, sender: h.Sender, usingJSON: IsUsingJSON()}
		go rspHandler.handleResponse(&h.wg, c)
	}
}

func (h *ConnectionHandlerFast) finish() { h.wg.Wait() }
