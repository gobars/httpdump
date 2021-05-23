package main

import (
	"fmt"
	"io"
	"os"
)

// Printer output parsed http messages
type Printer struct {
	outputQueue chan string
	outputFile  io.WriteCloser
	discarded   int
}

var maxOutputQueueLen = 4096

func newPrinter(outputPath string) *Printer {
	var outputFile io.WriteCloser
	if outputPath == "" {
		outputFile = os.Stdout
	} else {
		var err error
		outputFile, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
		if err != nil {
			panic(err)
		}

	}
	printer := &Printer{outputQueue: make(chan string, maxOutputQueueLen), outputFile: outputFile}
	printer.start()
	return printer
}

func (p *Printer) send(msg string) {
	select {
	case p.outputQueue <- msg:
	default:
		p.discarded++
	}
}

func (p *Printer) start() {
	printerWaitGroup.Add(1)
	go p.printBackground()
}

func (p *Printer) printBackground() {
	defer printerWaitGroup.Done()
	defer p.outputFile.Close()
	for msg := range p.outputQueue {
		_, _ = p.outputFile.Write([]byte(msg))
	}
}

func (p *Printer) finish() {
	close(p.outputQueue)
	fmt.Printf("#%d discarded\n", p.discarded)
}
