package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Printer output parsed http messages
type Printer struct {
	queue     chan string
	writer    io.Writer
	discarded uint32

	wg             sync.WaitGroup
	closer         func()
	delayDiscarded *DelayChan
}

func newPrinter(ctx context.Context, outputPath string, outChanSize uint) *Printer {
	w, closer := createWriter(outputPath)
	p := &Printer{queue: make(chan string, outChanSize), writer: w, closer: closer}
	p.delayDiscarded = NewDelayChan(ctx, func(v interface{}) {
		p.trySend(fmt.Sprintf("\n Discarded: %d\n", v.(uint32)))
	}, 10*time.Second)
	p.start(ctx)
	return p
}

func createWriter(outputPath string) (io.Writer, func()) {
	if outputPath == "" {
		return os.Stdout, func() {}
	}

	w, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		panic(err)
	}

	bw := bufio.NewWriter(w)
	return bw, func() { bw.Flush(); w.Close() }
}

func (p *Printer) trySend(msg string) {
	select {
	case p.queue <- msg:
	default:
	}
}

func (p *Printer) Send(msg string) {
	select {
	case p.queue <- msg:
	default:
		discarded := atomic.AddUint32(&p.discarded, 1)
		p.delayDiscarded.Put(discarded)
	}
}

func (p *Printer) start(ctx context.Context) {
	p.wg.Add(1)
	go p.printBackground(ctx)
}

func (p *Printer) printBackground(ctx context.Context) {
	defer p.wg.Done()
	defer p.closer()

	for {
		select {
		case msg, ok := <-p.queue:
			if !ok {
				return
			}
			_, _ = p.writer.Write([]byte(msg))
		case <-ctx.Done():
			return
		}
	}
}

func (p *Printer) finish() {
	p.queue <- fmt.Sprintf("\n#%d discarded", atomic.LoadUint32(&p.discarded))
	close(p.queue)
	p.wg.Wait()
}
