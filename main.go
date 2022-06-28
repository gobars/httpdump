package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/bingoohuang/gg/pkg/netx/freeport"
	"github.com/bingoohuang/gg/pkg/osx"

	"github.com/bingoohuang/gg/pkg/codec"
	"github.com/bingoohuang/gg/pkg/flagparse"
	"github.com/bingoohuang/gg/pkg/rest"
	"github.com/bingoohuang/gg/pkg/rotate"
	"github.com/bingoohuang/gg/pkg/v"
	"github.com/bingoohuang/golog"
	"github.com/bingoohuang/jj"

	"github.com/bingoohuang/httpdump/handler"
	"github.com/bingoohuang/httpdump/replay"
	"github.com/bingoohuang/httpdump/util"
	"github.com/google/gopacket/tcpassembly"

	"github.com/bingoohuang/gg/pkg/sigx"
)

// VersionInfo prints version information.
func (App) VersionInfo() string { return v.Version() }

func main() {
	app := &App{}
	flagparse.Parse(app, flagparse.AutoLoadYaml("c", "httpdump.yml"),
		flagparse.ProcessInit(&initAssets))

	defer golog.Setup().OnExit()

	app.print()
	app.handlerOption = &handler.Option{
		Resp:     app.Resp,
		Host:     app.Host,
		Uri:      app.URI,
		Method:   app.Method,
		Status:   app.Status,
		Level:    app.Level,
		DumpBody: app.DumpBody,
		DumpMax:  app.dumpMax,
		Force:    app.Force,
		Curl:     app.Curl,
		Eof:      app.Eof,
		Debug:    app.Debug,
		N:        app.N,
		Num:      app.N,
	}

	if app.Rate > 0 {
		app.handlerOption.RateLimiter = rate.NewLimiter(rate.Every(time.Duration(1e6/(app.Rate))*time.Microsecond), 1)
	}
	app.run()
}

//go:embed initassets
var initAssets embed.FS

// App Command line options.
type App struct {
	Config string `flag:"c" usage:"yaml config filepath"`
	Init   bool   `usage:"init example httpdump.yml/ctl and then exit"`
	Level  string `val:"all" usage:"Output level, url: only url, header: http headers, all: headers and text http body"`
	Input  string `flag:"i" val:"any" usage:"Interface name or pcap file. If not set, If is any, capture all interface traffics"`

	IP   string `usage:"Filter by ip, if either src or dst ip is matched, the packet will be processed"`
	Port uint   `usage:"Filter by port, if either source or target port is matched, the packet will be processed"`
	N    int32  `usage:"Max Requests and Responses captured, and then exits"`
	Bpf  string `usage:"Customized bpf, if it is set, -ip -port will be suppressed, e.g. tcp and ((dst host 1.2.3.4 and port 80) || (src host 1.2.3.4 and src port 80))"`

	Chan    uint `val:"10240" usage:"Channel size to buffer tcp packets"`
	OutChan uint `val:"40960" usage:"Output channel size to buffer tcp packets"`

	Host    string `usage:"Filter by request host, using wildcard match(*, ?)"`
	URI     string `usage:"Filter by request url path, using wildcard match(*, ?)"`
	Method  string `usage:"Filter by request method, multiple by comma"`
	Verbose string `usage:"Verbose flag, available req/rsp/all for http replay dump"`

	Status util.IntSetFlag `usage:"Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400"`

	Web        bool   `usage:"Start web server for HTTP requests and responses event"`
	WebPort    int    `usage:"Web server port if web is enable"`
	WebContext string `usage:"Web server context path if web is enable"`
	Resp       bool   `usage:"Print response or not"`
	Force      bool   `usage:"Force print unknown content-type http body even if it seems not to be text content"`
	Curl       bool   `usage:"Output an equivalent curl command for each http request"`
	Version    bool   `flag:"v" usage:"Print version info and exit"`
	Eof        bool   `val:"true" usage:"Output EOF connection info or not."`
	Debug      bool   `usage:"Enable debugging."`

	DumpBody string   `usage:"Prefix file of dump http request/response body, empty for no dump, like solr, solr:10 (max 10)"`
	Mode     string   `val:"fast" usage:"std/fast"`
	Output   []string `usage:"\n        File output, like dump-yyyy-MM-dd-HH-mm.http, suffix like :32m for max size, suffix :append for append mode\n        Or Relay http address, eg http://127.0.0.1:5002\n        Or any of stdout/stderr/stdout:log"`

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

	Pprof string `usage:"pprof address to listen on, not activate pprof if empty, eg. :6060"`

	Rate float64 `usage:"rate limit output per second"`

	handlerOption *handler.Option
}

func (o *App) run() {
	ctx, ctxCancel := sigx.RegisterSignals(nil)
	o.handlerOption.CtxCancel = ctxCancel
	sigx.RegisterSignalProfile()
	wg := &sync.WaitGroup{}

	if len(o.Output) == 0 {
		o.Output = []string{"stdout:log"}
	}

	senders := make(handler.Senders, 0, len(o.Output))
	for _, out := range o.Output {
		if addr, ok := rest.MaybeURL(out); ok {
			senders = append(senders, replay.CreateSender(ctx, wg, o.Method, o.File, o.Verbose, addr, o.OutChan))
		} else {
			senders = append(senders, rotate.NewQueueWriter(out,
				rotate.WithContext(ctx), rotate.WithOutChanSize(int(o.OutChan)), rotate.WithAppend(true)))
		}
	}

	if o.Web {
		var port int
		if o.WebPort > 0 {
			port = freeport.PortStart(o.WebPort)
		} else {
			port = freeport.Port()
		}

		stream := NewSSEStream()
		contextPath := path.Join("/", o.WebContext)
		log.Printf("contextPath: %s", contextPath)

		http.Handle("/", http.HandlerFunc(SSEWebHandler(contextPath, stream)))
		senders = append(senders, &SSESender{stream: stream})
		log.Printf("start to listen on %d", port)
		go func() {
			addr := fmt.Sprintf(":%d", port)
			if err := http.ListenAndServe(addr, nil); err != nil {
				log.Printf("listen and serve failed: %v", err)
			}
		}()
		go osx.OpenBrowser(fmt.Sprintf("http://127.0.0.1:%d%s", port, contextPath))
	}

	if o.File == "" {
		packets, err := util.CreatePacketsChan(o.Input, o.Bpf, o.Host, o.IP, o.Port)
		if err != nil {
			panic(err)
		}
		go util.LoopPackets(ctx, packets, o.createAssembler(ctx, senders), o.Idle)
	}

	<-ctx.Done()
	log.Printf("sleep 3s and then exit...")
	time.Sleep(3 * time.Second)
	_ = senders.Close()
	wg.Wait()
}

func (o *App) createAssembler(ctx context.Context, sender handler.Sender) util.Assembler {
	switch o.Mode {
	case "fast":
		h := &handler.ConnectionHandlerFast{Context: ctx, Option: o.handlerOption, Sender: sender}
		return handler.NewTCPAssembler(h, o.Chan, o.IP, uint16(o.Port), o.Resp)
	default:
		return o.createTCPStdAssembler(ctx, sender)
	}
}

func (o *App) createTCPStdAssembler(ctx context.Context, printer handler.Sender) *handler.TcpStdAssembler {
	f := handler.NewFactory(ctx, o.handlerOption, printer)
	p := tcpassembly.NewStreamPool(f)
	assembler := tcpassembly.NewAssembler(p)
	return &handler.TcpStdAssembler{Assembler: assembler}
}

// PostProcess does some post processes.
func (o *App) PostProcess() {
	o.processDumpBody()
}

func (o *App) processDumpBody() {
	if o.DumpBody == "" {
		return
	}

	p := strings.Index(o.DumpBody, ":")
	if p < 0 {
		return
	}

	if a, err := strconv.Atoi(o.DumpBody[p+1:]); err == nil {
		o.dumpMax = uint32(a)
	}

	if o.DumpBody = o.DumpBody[:p]; o.DumpBody == "" {
		o.DumpBody = "dump"
	}
}

func (o App) print() {
	s := codec.Json(o)
	s, _ = jj.SetBytes(s, "Idle", o.Idle.String())
	log.Printf("Options: %s", s)
}
