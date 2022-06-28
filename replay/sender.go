package replay

import (
	"context"
	"log"
	"sync"
)

type Sender struct {
	ch chan string
}

func (ss *Sender) Close() error {
	close(ss.ch)
	return nil
}

func (ss *Sender) Send(msg string, countDiscards bool) {
	if !countDiscards {
		return
	}
	ss.ch <- msg
}

func CreateSender(ctx context.Context, wg *sync.WaitGroup, method, file, verbose, addr string, chanSize uint,
	replayN int, replayFraction float64,
) *Sender {
	rc := Config{Method: method, File: file, Verbose: verbose, Replay: addr, ReplayN: replayN, ReplayFraction: replayFraction}
	ch := make(chan string, chanSize)
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := rc.StartReplay(ctx, ch); err != nil {
			log.Printf("E! start replay err: %v", err)
		}
	}()

	return &Sender{ch: ch}
}
