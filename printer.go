package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// Printer output parsed http messages
type Printer struct {
	queue     chan string
	writer    io.Writer
	discarded uint32

	wg     sync.WaitGroup
	closer func()
}

func newPrinter(outputPath string, outChanSize uint) *Printer {
	w, closer := createWriter(outputPath)
	p := &Printer{queue: make(chan string, outChanSize), writer: w, closer: closer}
	p.start()
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

func (p *Printer) Send(msg string) {
	select {
	case p.queue <- msg:
	default:
		atomic.AddUint32(&p.discarded, 1)
	}
}

func (p *Printer) start() {
	p.wg.Add(1)
	go p.printBackground()
}

func (p *Printer) printBackground() {
	defer p.wg.Done()
	defer p.closer()

	for msg := range p.queue {
		_, _ = p.writer.Write([]byte(msg))
	}
}

func (p *Printer) finish() {
	close(p.queue)
	p.wg.Wait()
	fmt.Printf("\n#%d discarded", atomic.LoadUint32(&p.discarded))
}
