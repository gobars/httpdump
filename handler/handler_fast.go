package handler

import (
	"context"
	"sync"

	"github.com/allegro/bigcache/v3"
)

// ConnectionHandlerFast impl ConnectionHandler
type ConnectionHandlerFast struct {
	context.Context
	Option *Option
	Sender Sender
	wg     sync.WaitGroup
	cache  *bigcache.BigCache
}

func (h *ConnectionHandlerFast) handle(src Endpoint, dst Endpoint, c *TCPConnection) {
	b := NewBase(h.Context, &ConnectionKey{src: src, dst: dst}, h.Option, h.Sender)

	h.wg.Add(1)
	go b.handleRequest(&h.wg, c)

	if h.Option.Resp > 0 {
		h.wg.Add(1)
		go b.handleResponse(&h.wg, c)
	}
}

func (h *ConnectionHandlerFast) finish() { h.wg.Wait() }
