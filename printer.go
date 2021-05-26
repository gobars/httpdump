package main

import (
	"context"
	"fmt"
	"github.com/bingoohuang/gg/pkg/man"
	"io"
	"os"
	"strings"
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
	s := strings.ReplaceAll(outputPath, ":append", "")
	appendMode := s != outputPath
	maxSize := uint64(0)
	if pos := strings.LastIndex(s, ":"); pos > 0 {
		maxSize, _ = man.ParseBytes(s[pos+1:])
		s = s[:pos]
	}

	w, closer := createWriter(s, maxSize, appendMode)
	p := &Printer{queue: make(chan string, outChanSize), writer: w, closer: closer}
	p.delayDiscarded = NewDelayChan(ctx, func(v interface{}) {
		p.Send(fmt.Sprintf("\n discarded: %d\n", v.(uint32)), false)
	}, 10*time.Second)
	p.start(ctx)
	return p
}

func createWriter(outputPath string, maxSize uint64, append bool) (io.Writer, func()) {
	if outputPath == "" {
		return os.Stdout, func() {}
	}

	bw := NewRotateFileWriter(outputPath, maxSize, append)
	return bw, func() { _ = bw.Close() }
}

func (p *Printer) Send(msg string, countDiscards bool) {
	defer func() {
		recover()
	}()
	if msg == "" {
		return
	}

	select {
	case p.queue <- msg:
	default:
		if countDiscards {
			discarded := atomic.AddUint32(&p.discarded, 1)
			p.delayDiscarded.Put(discarded)
		}
	}
}

func (p *Printer) start(ctx context.Context) {
	p.wg.Add(1)
	go p.printBackground(ctx)
}

func (p *Printer) printBackground(ctx context.Context) {
	defer p.wg.Done()
	defer p.closer()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-p.queue:
			if !ok {
				return
			}
			_, _ = p.writer.Write([]byte(msg))
			ticker.Reset(1 * time.Second)
		case <-ticker.C:
			if f, ok := p.writer.(Flusher); ok {
				_ = f.Flush()
			}
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
