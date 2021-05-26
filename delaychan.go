package main

import (
	"context"
	"time"
)

func NewDelayChan(ctx context.Context, fn func(interface{}), delay time.Duration) *DelayChan {
	d := &DelayChan{C: make(chan interface{}, 1), fn: fn}
	go d.run(ctx, delay)
	return d
}

type DelayChan struct {
	C  chan interface{}
	fn func(interface{})
}

func (c *DelayChan) run(ctx context.Context, delay time.Duration) {
	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.tryPutIdleConn()
		case <-ctx.Done():
			return
		}
	}
}

func (c *DelayChan) tryPutIdleConn() {
	select {
	case v := <-c.C:
		c.fn(v)
	default:
	}
}

func (c *DelayChan) Put(v interface{}) {
	// try to remove old one.
	select {
	case <-c.C:
	default:
	}

	// try to put the new one.
	select {
	case c.C <- v:
	default:
	}
}
