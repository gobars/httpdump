package replay

import (
	"context"
	"fmt"
	"github.com/bingoohuang/httpdump/globpath"
	"github.com/influxdata/tail"
	"golang.org/x/sync/errgroup"
	"log"
	"sync"
	"time"
)

type Tail struct {
	filepath      string
	fromBeginning bool
	options       *Options

	wg    sync.WaitGroup
	lines chan []byte
	poll  bool
}

func (t *Tail) TailPayloads(ctx context.Context) error {
	tailers := make(map[string]*tail.Tail)
	offsets := make(map[string]int64)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	if err := t.tailNewFiles(ctx, tailers, offsets); err != nil {
		log.Printf("E! tail new files: %v", err)
	}

	defer t.wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := t.tailNewFiles(ctx, tailers, offsets); err != nil {
				log.Printf("E! tail new files: %v", err)
			}
		}
	}

}

func (t *Tail) tailNewFiles(ctx context.Context, tailers map[string]*tail.Tail, offsets map[string]int64) error {
	g, err := globpath.Compile(t.filepath)
	if err != nil {
		return fmt.Errorf("glob %q failed to compile: %s", t.filepath, err.Error())
	}

	for _, file := range g.Match() {
		if _, ok := tailers[file]; ok {
			// we're already tailing this file
			continue
		}

		var seek *tail.SeekInfo
		if !t.fromBeginning {
			if offset, ok := offsets[file]; ok {
				log.Printf("Using offset %d for %q", offset, file)
				seek = &tail.SeekInfo{Whence: 0, Offset: offset}
			} else {
				seek = &tail.SeekInfo{Whence: 2, Offset: 0}
			}
		}

		tailer, err := tail.TailFile(file,
			tail.Config{
				ReOpen:    true,
				Follow:    true,
				Location:  seek,
				MustExist: true,
				Poll:      t.poll, // poll
				Pipe:      false,
				Logger:    tail.DiscardingLogger,
			})

		if err != nil {
			log.Printf("Failed to open file (%s): %v", file, err)
			continue
		}

		log.Printf("Tail added for %q", file)

		// create a goroutine for each "tailer"
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.receiver(ctx, tailer)

			log.Printf("Tail removed for %q", tailer.Filename)

			if err := tailer.Err(); err != nil {
				log.Printf("Tailing %q: %s", tailer.Filename, err.Error())
			}
		}()

		tailers[tailer.Filename] = tailer
	}
	return nil
}

// Receiver is launched as a goroutine to continuously watch a tailed logfile
// for changes, parse any incoming msgs, and add to the accumulator.
func (t *Tail) receiver(ctx context.Context, tailer *tail.Tail) {
	lines := make(chan []byte)
	var g errgroup.Group

	g.Go(func() error {
		return t.options.ConsumePayloadLines(lines)
	})

	g.Go(func() error {
		return t.tailing(ctx, tailer, lines)
	})

	if err := g.Wait(); err != nil {
		log.Printf("E! failed to wait: %v", err)
	}
}

func (t *Tail) tailing(ctx context.Context, tailer *tail.Tail, lines chan []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, _ := <-tailer.Lines:
			if line != nil {
				if line.Text != "" {
					lines <- []byte(line.Text + "\n")
				}

				if line.Err != nil {
					log.Printf("E! Tailing %q: %s", tailer.Filename, line.Err.Error())
					return line.Err
				}
			}
		}
	}
}
