package replay

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"

	"golang.org/x/sync/errgroup"
)

type (
	StarterFn      func(data []byte) bool
	TerminatorFn   func(data []byte) bool
	PayloadHandler func(payload Msg) error
)

type Options struct {
	Starter        StarterFn
	Terminator     TerminatorFn
	Handler        PayloadHandler
	IncludingStart bool
	IncludingEnd   bool
}

func PayloadPrinter(payload Msg) error {
	log.Printf("Payload title:%s <<<\n%s\n>>>", payload.Title, payload.Data)
	return nil
}

func (o *Options) ReadPayloads(r io.Reader) error {
	var g errgroup.Group

	lines := make(chan []byte)
	g.Go(func() error {
		return ProducePayloads(r, lines)
	})

	g.Go(func() error {
		return o.ConsumePayloadLines(lines)
	})

	return g.Wait()
}

func ProducePayloads(r io.Reader, ch chan<- []byte) error {
	scanner := bufio.NewScanner(r)
	scanner.Split(ScanLines)

	for scanner.Scan() {
		ch <- scanner.Bytes()
	}

	close(ch)

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

type msg struct {
	Title []byte
	bytes.Buffer
}

type Msg struct {
	Title []byte
	Data  []byte
}

func (b *msg) tryParsePayload(payloadHandler PayloadHandler) error {
	if b != nil && b.Len() > 0 {
		if err := payloadHandler(Msg{Title: b.Title, Data: b.Bytes()}); err != nil {
			return err
		}
	}
	return nil
}

func (o *Options) ConsumePayloadLines(ch <-chan []byte) error {
	b := new(msg)

	if o.Handler == nil {
		o.Handler = PayloadPrinter
	}

	started := false
	var last []byte

	for pack := range ch {
		if o.Starter != nil && o.Starter(pack) {
			if started && o.Terminator == nil {
				if err := b.tryParsePayload(o.Handler); err != nil {
					return err
				}
			}
			started = true
			b = &msg{Title: last}
			if o.IncludingStart {
				b.Write(pack)
			}
		} else if o.Terminator != nil && o.Terminator(pack) {
			if o.Starter == nil || started {
				if err := b.tryParsePayload(o.Handler); err != nil {
					return err
				}
			}
			b = new(msg)
			if o.IncludingEnd {
				b.Write(pack)
			}
			started = false
		} else if started || o.Terminator != nil {
			b.Write(pack)
		}

		last = pack
	}

	if started && o.Terminator == nil {
		if err := b.tryParsePayload(o.Handler); err != nil {
			return err
		}
	}

	return nil
}

func ScanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		// We have a full newline-terminated line.
		return i + 1, data[0 : i+1], nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}
