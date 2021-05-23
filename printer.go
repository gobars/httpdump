package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// Printer output parsed http messages
type Printer struct {
	queue     chan string
	writer    io.WriteCloser
	discarded int

	wg sync.WaitGroup
}

func newPrinter(outputPath string) *Printer {
	var err error
	var w io.WriteCloser
	if outputPath == "" {
		w = os.Stdout
	} else if w, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666); err != nil {
		panic(err)
	}
	p := &Printer{queue: make(chan string, 4096), writer: w}
	p.start()
	return p
}

func (p *Printer) send(msg string) {
	select {
	case p.queue <- msg:
	default:
		p.discarded++
	}
}

func (p *Printer) start() {
	p.wg.Add(1)
	go p.printBackground()
}

func (p *Printer) printBackground() {
	p.wg.Done()
	defer p.writer.Close()
	for msg := range p.queue {
		_, _ = p.writer.Write([]byte(msg))
	}
}

func (p *Printer) finish() {
	close(p.queue)
	p.wg.Wait()
	fmt.Printf("#%d discarded\n", p.discarded)
}
