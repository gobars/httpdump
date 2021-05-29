package replay

import (
	"context"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bingoohuang/gg/pkg/rest"
	"github.com/bingoohuang/httpdump/globpath"
	"go.uber.org/multierr"
)

type Config struct {
	File           string
	Replay         string
	Method         string
	Timeout        time.Duration
	RedirectLimit  int
	InsecureVerify bool
	Poll           bool
	Verbose        string
}

func (c *Config) StartReplay(ctx context.Context, payloadCh <-chan string) error {
	options := c.createParseOptions()

	if c.File != "" {
		file := strings.ReplaceAll(c.File, ":tail", "")
		tail := file != c.File
		c.File = file

		file = strings.ReplaceAll(c.File, ":poll", "")
		c.Poll = file != c.File
		c.File = file

		if dir, e := os.Stat(c.File); e == nil && dir.IsDir() {
			return c.processDir(options)
		}

		if tail {
			return c.processTail(ctx, options)
		}

		return c.processGlob(options)
	}

	for payload := range payloadCh {
		if err := options.ReadPayloads(strings.NewReader(payload)); err != nil {
			log.Printf("E! failed to read payloads, error: %v", err)
		}
	}

	return nil
}

func (c *Config) processTail(ctx context.Context, parseOptions *Options) error {
	t := &Tail{
		filepath:      c.File,
		poll:          c.Poll,
		fromBeginning: false,
		options:       parseOptions,
	}

	return t.TailPayloads(ctx)
}

func (c *Config) processGlob(parseOptions *Options) error {
	glob, err := globpath.Compile(c.File)
	if err != nil {
		return err
	}

	for _, file := range glob.Match() {
		log.Printf("Processing file %s", file)
		if pe := c.processFile(file, parseOptions); pe != nil {
			err = multierr.Append(err, pe)
		}
	}

	return err
}

func (c *Config) processDir(options *Options) error {
	return filepath.WalkDir(c.File, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		log.Printf("Processing file %s", path)
		return c.processFile(path, options)
	})
}

func (c *Config) processFile(file string, options *Options) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}

	defer f.Close()

	return options.ReadPayloads(f)
}

func (c *Config) createParseOptions() *Options {
	payloadHandler := func(Msg) error { return nil }
	if v := c.CreateHTTPClientConfig(); v != nil {
		client := v.NewHTTPClient()
		payloadHandler = func(payload Msg) error {
			return replay(client, payload)
		}
	}

	return &Options{
		Starter: func(data []byte) bool {
			_, _, ok := ParseRequestTitle(data)
			return ok
		},
		IncludingStart: true,
		Handler:        payloadHandler,
	}
}

const layout = `2006-01-02 15:04:05.000000`

func replay(client *HTTPClient, payload Msg) error {
	logTitle(payload.Title, "", "")
	if r, err := client.Send(payload.Data); err != nil {
		log.Printf("E! Failed to replay, error %v", err)
	} else if r != nil {
		log.Printf("Replay %s %s, cost %s, status: %d", r.Method, r.URL, r.Cost, r.StatusCode)
	}
	return nil
}

var timeUnixNano = regexp.MustCompile(`\d{19,}`)

func logTitle(title []byte, method, uri string) {
	if len(title) == 0 {
		return
	}

	s := strings.TrimSpace(string(title))

	// 1 fda9138b7f0000016ac0ad3e 1621835869410250000 0
	if found := timeUnixNano.FindString(s); found != "" {
		if nano, err := strconv.ParseInt(found, 10, 64); err == nil {
			tim := time.Unix(0, nano).Format(layout)
			if method == "" {
				log.Printf("Timestamp: %s Title:%s", tim, s)
				return
			}

			if u, _ := url.Parse(uri); u != nil {
				uri = u.Path
			}
			log.Printf("Timestamp: %s Title: %s Method: %s, URI: %s", tim, s, method, uri)
			return
		}
	}

	log.Printf("Title: %s", s)
}

func (c *Config) CreateHTTPClientConfig() *HTTPClientConfig {
	if c.Replay == "" {
		return nil
	}

	if _, err := rest.FixURI(c.Replay); err != nil {
		panic(err)
	}
	u, err := url.Parse(c.Replay)
	if err != nil {
		panic(err)
	}

	return &HTTPClientConfig{
		Timeout:        c.Timeout,
		InsecureVerify: c.InsecureVerify,
		BaseURL:        u,
		Methods:        c.Method,
		Verbose:        c.Verbose,
	}
}
