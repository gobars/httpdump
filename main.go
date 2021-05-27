package main

import (
	"context"
	"fmt"
	"github.com/google/gopacket/tcpassembly"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bingoohuang/gg/pkg/ctx"
	"github.com/bingoohuang/gg/pkg/flagparse"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

const (
	LevelL0     = "l0"
	LevelUrl    = "url"
	LevelHeader = "header"
	LevelAll    = "all"
)

// Option Command line options.
type Option struct {
	Level    string        `val:"header" usage:"Output level, options are: l1(first line) | url(only url) | header(http headers) | all(headers, and textuary http body)"`
	Input    string        `flag:"i" val:"any" usage:"Interface name or pcap file. If not set, If is any, capture all interface traffics"`
	Ip       string        `usage:"Filter by ip, if either src or dst ip is matched, the packet will be processed"`
	Port     uint          `usage:"Filter by port, if either source or target port is matched, the packet will be processed"`
	Bpf      string        `usage:"Customized bpf, if it is set, -ip -port will be suppressed"`
	Chan     uint          `val:"10240" usage:"Channel size to buffer tcp packets"`
	OutChan  uint          `val:"40960" usage:"Output channel size to buffer tcp packets"`
	Host     string        `usage:"Filter by request host, using wildcard match(*, ?)"`
	Uri      string        `usage:"Filter by request url path, using wildcard match(*, ?)"`
	Method   string        `usage:"Filter by request method, multiple by comma"`
	Resp     bool          `usage:"Print response or not"`
	Version  bool          `flag:"v" usage:"Print version info and exit"`
	Status   Status        `usage:"Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400"`
	Force    bool          `usage:"Force print unknown content-type http body even if it seems not to be text content"`
	Curl     bool          `usage:"Output an equivalent curl command for each http request"`
	DumpBody string        `usage:"Prefix file of dump http request/response body, empty for no dump, like solr, solr:10 (max 10)"`
	Mode     string        `val:"fast" usage:"std/fast/pair"`
	Output   string        `usage:"File to write result, like dump-yyyy-MM-dd-HH-mm.http, suffix like :32m for max size, suffix :append for append mode"`
	Idle     time.Duration `val:"4m" usage:"Idle time to remove connection if no package received"`

	dumpMax uint32
	dumpNum uint32
}

func (Option) VersionInfo() string { return "httpdump v1.1.0 2021-05-27 09:39:10" }

func main() {
	option := &Option{}
	flagparse.Parse(option)
	option.PostProcess()

	if err := option.run(); err != nil {
		panic(err)
	}
}

func (o *Option) run() error {
	if o.Port > 65536 {
		return fmt.Errorf("ignored invalid port %v", o.Port)
	}

	packets, err := createPacketsChan(o.Input, o.Bpf, o.Host, o.Ip, o.Port)
	if err != nil {
		return err
	}

	c, _ := ctx.RegisterSignals(nil)
	printer := newPrinter(c, o.Output, o.OutChan)

	var assembler Assembler

	switch o.Mode {
	case "fast", "pair":
		a := newTCPAssembler(o.createConnectionHandler(printer), o.Resp)
		a.chanSize = o.Chan
		a.filterIP = o.Ip
		a.filterPort = uint16(o.Port)
		assembler = a
	default:
		assembler = createTcpStdAssembler(c, o, printer)
	}

	loop(c, packets, assembler, o.Idle)

	assembler.FinishAll()
	printer.finish()
	return nil
}

func createTcpStdAssembler(c context.Context, o *Option, printer *Printer) *TcpStdAssembler {
	assembler := tcpassembly.NewAssembler(tcpassembly.NewStreamPool(NewFactory(c, o, printer)))
	return &TcpStdAssembler{Assembler: assembler}
}

type Sender interface {
	Send(msg string, countDiscards bool)
}

func (o *Option) createConnectionHandler(sender Sender) ConnectionHandler {
	if o.Mode == "fast" {
		return &ConnectionHandlerFast{option: o, sender: sender}
	}

	return &ConnectionHandlerPair{option: o, sender: sender}
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

func (o *Option) CanDump() bool {
	if o.DumpBody == "" {
		return false
	}

	return o.dumpMax <= 0 || atomic.LoadUint32(&o.dumpNum) < o.dumpMax
}

type Assembler interface {
	Assemble(flow gopacket.Flow, tcp *layers.TCP, timestamp time.Time)
	FlushOlderThan(time time.Time)
	FinishAll()
}

func loop(ctx context.Context, packets chan gopacket.Packet, assembler Assembler, idle time.Duration) {
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case p := <-packets:
			if p == nil { // A nil p indicates the end of a pcap file.
				return
			}

			// only assembly tcp/ip packets
			n, t := p.NetworkLayer(), p.TransportLayer()
			if n == nil || t == nil || t.LayerType() != layers.LayerTypeTCP {
				continue
			}

			assembler.Assemble(n.NetworkFlow(), t.(*layers.TCP), p.Metadata().Timestamp)
		case <-ticker.C:
			// flush connections that haven't been activity in the idle time
			assembler.FlushOlderThan(time.Now().Add(-idle))
		case <-ctx.Done():
			return
		}
	}
}

type Status IntSet

func (i *Status) String() string { return "" }

func (i *Status) Set(value string) error {
	set, err := ParseIntSet(value)
	if err != nil {
		return err
	}
	*i = Status(*set)
	return nil
}
