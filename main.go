package main

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bingoohuang/gg/pkg/rest"
	"github.com/bingoohuang/gg/pkg/ss"
	"github.com/bingoohuang/httpdump/handler"
	"github.com/bingoohuang/httpdump/replay"
	"github.com/bingoohuang/httpdump/util"
	"github.com/google/gopacket/tcpassembly"

	"github.com/bingoohuang/gg/pkg/ctx"
	"github.com/bingoohuang/gg/pkg/flagparse"
)

// Option Command line options.
type Option struct {
	Level string `val:"all" usage:"Output level, l1: first line, url: only url, header: http headers, all: headers and text http body"`

	Input string `flag:"i" val:"any" usage:"Interface name or pcap file. If not set, If is any, capture all interface traffics"`

	Ip   string `usage:"Filter by ip, if either src or dst ip is matched, the packet will be processed"`
	Port uint   `usage:"Filter by port, if either source or target port is matched, the packet will be processed"`
	Bpf  string `usage:"Customized bpf, if it is set, -ip -port will be suppressed"`

	Chan    uint `val:"10240" usage:"Channel size to buffer tcp packets"`
	OutChan uint `val:"40960" usage:"Output channel size to buffer tcp packets"`

	Host    string `usage:"Filter by request host, using wildcard match(*, ?)"`
	Uri     string `usage:"Filter by request url path, using wildcard match(*, ?)"`
	Method  string `usage:"Filter by request method, multiple by comma"`
	Verbose string `usage:"Verbose flag, available req/rsp/all for http replay dump"`

	Status util.IntSetFlag `usage:"Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400"`

	Resp    bool `usage:"Print response or not"`
	Force   bool `usage:"Force print unknown content-type http body even if it seems not to be text content"`
	Curl    bool `usage:"Output an equivalent curl command for each http request"`
	Version bool `flag:"v" usage:"Print version info and exit"`

	DumpBody string   `usage:"Prefix file of dump http request/response body, empty for no dump, like solr, solr:10 (max 10)"`
	Mode     string   `val:"fast" usage:"std/fast/pair"`
	Output   []string `usage:"File output, like dump-yyyy-MM-dd-HH-mm.http, suffix like :32m for max size, suffix :append for append mode\n Or Relay http address, eg http://127.0.0.1:5002"`

	Idle time.Duration `val:"4m" usage:"Idle time to remove connection if no package received"`

	dumpMax uint32

	// https://github.com/influxdata/telegraf/blob/master/plugins/inputs/tail/tail.go
	//  ## File names or a pattern to tail.
	//  ## These accept standard unix glob matching rules, but with the addition of
	//  ## ** as a "super asterisk". ie:
	//  ##   "/var/log/**.log"  -> recursively find all .log files in /var/log
	//  ##   "/var/log/*/*.log" -> find all .log files with a parent dir in /var/log
	//  ##   "/var/log/apache.log" -> just tail the apache log file
	//  ##   "/var/log/log[!1-2]*  -> tail files without 1-2
	//  ##   "/var/log/log[^1-2]*  -> identical behavior as above
	File string `flag:"f" usage:"File of http request to parse, glob pattern like data/*.gor, or path like data/, suffix :tail to tail files, suffix :poll to set the tail watch method to poll"`

	handlerOption *handler.Option
}

func (Option) VersionInfo() string { return "httpdump v1.2.4 2021-05-29 07:48:43" }

func main() {
	option := &Option{}
	flagparse.Parse(option)

	option.handlerOption = option.createHandleOption()
	option.run()
}

func (o *Option) run() {
	c, _ := ctx.RegisterSignals(nil)
	rc := replay.Config{Method: o.Method, File: o.File, Verbose: o.Verbose}

	var wg sync.WaitGroup

	if len(o.Output) == 0 {
		o.Output = []string{"stdout"}
	}
	senders := make(handler.Senders, 0, len(o.Output))
	for _, out := range o.Output {
		if addr, ok := IsURL(out); ok {
			rc.Replay = addr
			ch := make(chan string, o.OutChan)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := rc.StartReplay(c, ch); err != nil {
					log.Printf("E! err: %v", err)
				}
			}()
			senders = append(senders, &ReplaySender{ch: ch})
		} else {
			w := util.NewRotateWriter(c, out, o.OutChan, true)
			senders = append(senders, w)
		}
	}

	if o.File == "" {
		packets, err := util.CreatePacketsChan(o.Input, o.Bpf, o.Host, o.Ip, o.Port)
		if err != nil {
			panic(err)
		}
		assembler := o.createAssembler(c, senders)
		util.LoopPackets(c, packets, assembler, o.Idle)
	}

	senders.Close()
	wg.Wait()
}

func IsURL(out string) (string, bool) {
	if out == "stdout" {
		return "", false
	}

	if ss.HasPrefix(out, "http://", "https://") {
		return out, true
	}

	if fn := util.ParseFileNameTemplate(out); fn != out {
		return "", false
	}

	if _, appendMode, maxSize := util.ParseOutputPath(out); appendMode || maxSize > 0 {
		return "", false
	}

	uri, err := rest.FixURI(out)
	return uri, err == nil
}

type ReplaySender struct {
	ch chan string
}

func (ss *ReplaySender) Close() error {
	close(ss.ch)
	return nil
}

func (ss *ReplaySender) Send(msg string, countDiscards bool) {
	if !countDiscards {
		return
	}
	ss.ch <- msg
}

func (o *Option) createAssembler(c context.Context, sender handler.Sender) util.Assembler {
	switch o.Mode {
	case "fast", "pair":
		h := o.createConnectionHandler(sender)
		return handler.NewTCPAssembler(h, o.Chan, o.Ip, uint16(o.Port), o.Resp)
	default:
		return o.createTcpStdAssembler(c, sender)
	}
}

func (o *Option) createTcpStdAssembler(c context.Context, printer handler.Sender) *handler.TcpStdAssembler {
	f := handler.NewFactory(c, o.handlerOption, printer)
	p := tcpassembly.NewStreamPool(f)
	assembler := tcpassembly.NewAssembler(p)
	return &handler.TcpStdAssembler{Assembler: assembler}
}

func (o *Option) createConnectionHandler(sender handler.Sender) handler.ConnectionHandler {
	if o.Mode == "fast" {
		return &handler.ConnectionHandlerFast{Option: o.handlerOption, Sender: sender}
	} else {
		return &handler.ConnectionHandlerPair{Option: o.handlerOption, Sender: sender}
	}
}

func (o *Option) PostProcess() {
	o.processDumpBody()
}

func (o *Option) processDumpBody() {
	if o.DumpBody == "" {
		return
	}

	p := strings.Index(o.DumpBody, ":")
	if p < 0 {
		return
	}

	if v, err := strconv.Atoi(o.DumpBody[p+1:]); err == nil {
		o.dumpMax = uint32(v)
	}

	if o.DumpBody = o.DumpBody[:p]; o.DumpBody == "" {
		o.DumpBody = "dump"
	}
}

func (o *Option) createHandleOption() *handler.Option {
	return &handler.Option{
		Resp:     o.Resp,
		Host:     o.Host,
		Uri:      o.Uri,
		Method:   o.Method,
		Status:   o.Status,
		Level:    o.Level,
		DumpBody: o.DumpBody,
		DumpMax:  o.dumpMax,
		Force:    o.Force,
		Curl:     o.Curl,
	}
}
