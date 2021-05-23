package main

import (
	"fmt"
	"time"

	"sync"

	"github.com/bingoohuang/gg/pkg/flagparse"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// Option Command line options.
type Option struct {
	Level     string        `val:"header" usage:"Output level, options are: url(only url) | header(http headers) | all(headers, and textuary http body)"`
	Input     string        `flag:"i" val:"any" usage:"Interface name or pcap file. If not set, If is any, capture all interface traffics"`
	Ip        string        `usage:"Filter by ip, if either source or target ip is matched, the packet will be processed"`
	Port      uint          `usage:"Filter by port, if either source or target port is matched, the packet will be processed."`
	Chan      uint          `val:"10240" usage:"Channel size to buffer tcp packets."`
	Host      string        `usage:"Filter by request host, using wildcard match(*, ?)"`
	Uri       string        `usage:"Filter by request url path, using wildcard match(*, ?)"`
	PrintResp bool          `usage:"Print response or not"`
	Status    Status        `usage:"Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400"`
	Force     bool          `usage:"Force print unknown content-type http body even if it seems not to be text content"`
	Curl      bool          `usage:"Output an equivalent curl command for each http request"`
	DumpBody  bool          `usage:"Dump http request/response body to file"`
	Fast      bool          `usage:"Fast mode, process request and response separately"`
	Output    string        `usage:"Write result to file [output] instead of stdout"`
	Idle      time.Duration `val:"4m" usage:"Idle time to remove connection if no package received"`
}

func main() {
	option := &Option{}
	flagparse.Parse(option)
	if err := run(option); err != nil {
		panic(err)
	}
}

var waitGroup sync.WaitGroup
var printerWaitGroup sync.WaitGroup

func run(option *Option) error {
	if option.Port > 65536 {
		return fmt.Errorf("ignored invalid port %v", option.Port)
	}

	packets, err := createPacketsChan(option.Input, option.Host, option.Ip, option.Port)
	if err != nil {
		return err
	}

	printer := newPrinter(option.Output)
	var handler ConnectionHandler
	if option.Fast {
		handler = &FastConnectionHandler{
			option:  option,
			printer: printer,
		}
	} else {
		handler = &HTTPConnectionHandler{
			option:  option,
			printer: printer,
		}
	}
	assembler := newTCPAssembler(handler)
	assembler.chanSize = option.Chan
	assembler.filterIP = option.Ip
	assembler.filterPort = uint16(option.Port)

	loop(packets, assembler, option.Idle)

	assembler.finishAll()
	waitGroup.Wait()
	printer.finish()
	printerWaitGroup.Wait()
	return nil
}

func loop(packets chan gopacket.Packet, assembler *TCPAssembler, idle time.Duration) {
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

			assembler.assemble(n.NetworkFlow(), t.(*layers.TCP), p.Metadata().Timestamp)
		case <-ticker.C:
			// flush connections that haven't been activity in the idle time
			assembler.flushOlderThan(time.Now().Add(-idle))
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
