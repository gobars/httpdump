package util

import (
	"context"
	"fmt"
	"github.com/bingoohuang/gg/pkg/man"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RotateWriter output parsed http messages
type RotateWriter struct {
	queue          chan string
	writer         io.Writer
	discarded      uint32
	allowDiscarded bool

	wg             sync.WaitGroup
	closer         func()
	delayDiscarded *DelayChan
}

func NewRotateWriter(ctx context.Context, outputPath string, outChanSize uint, allowDiscarded bool) *RotateWriter {
	s, appendMode, maxSize := ParseOutputPath(outputPath)
	w, closer := createWriter(s, maxSize, appendMode)
	p := &RotateWriter{
		queue:          make(chan string, outChanSize),
		writer:         w,
		closer:         closer,
		allowDiscarded: allowDiscarded,
	}

	if allowDiscarded {
		p.delayDiscarded = NewDelayChan(ctx, func(v interface{}) {
			p.Send(fmt.Sprintf("\n discarded: %d\n", v.(uint32)), false)
		}, 10*time.Second)
	}
	p.wg.Add(1)
	go p.printBackground(ctx)
	return p
}

func ParseOutputPath(outputPath string) (string, bool, uint64) {
	s := strings.ReplaceAll(outputPath, ":append", "")
	appendMode := s != outputPath
	maxSize := uint64(0)
	if pos := strings.LastIndex(s, ":"); pos > 0 {
		maxSize, _ = man.ParseBytes(s[pos+1:])
		s = s[:pos]
	}
	return s, appendMode, maxSize
}

func createWriter(outputPath string, maxSize uint64, append bool) (io.Writer, func()) {
	if outputPath == "stdout" {
		return os.Stdout, func() {}
	}

	bw := NewRotateFileWriter(outputPath, maxSize, append)
	return bw, func() { _ = bw.Close() }
}

func (p *RotateWriter) Send(msg string, countDiscards bool) {
	if msg == "" {
		return
	}

	defer func() {
		if err := recover(); err != nil {
			log.Printf("W! Recovered %v", err)
		}
	}() // avoid write to closed p.queue

	if !p.allowDiscarded {
		p.queue <- msg
		return
	}

	select {
	case p.queue <- msg:
	default:
		if countDiscards {
			p.delayDiscarded.Put(atomic.AddUint32(&p.discarded, 1))
		}
	}
}

func (p *RotateWriter) printBackground(ctx context.Context) {
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
		case <-ticker.C:
			if f, ok := p.writer.(Flusher); ok {
				_ = f.Flush()
			}
		case <-ctx.Done():
			return
		}
	}

}

func (p *RotateWriter) Close() error {
	if p.allowDiscarded {
		if val := atomic.LoadUint32(&p.discarded); val > 0 {
			p.queue <- fmt.Sprintf("\n#%d discarded", val)
		}
	}
	close(p.queue)
	p.wg.Wait()
	return nil
}
