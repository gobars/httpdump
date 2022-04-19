package main

import (
	"bufio"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"text/template"

	"github.com/AndrewBurian/eventsource"
	"github.com/bingoohuang/gg/pkg/codec"
	"github.com/bingoohuang/gg/pkg/man"
	"github.com/bingoohuang/gg/pkg/ss"
	"github.com/bingoohuang/httpdump/handler"
	"github.com/bingoohuang/httpdump/replay"
	"github.com/bingoohuang/httpdump/util"
)

//go:embed web
var web embed.FS

var webRoot = func() fs.FS {
	sub, err := fs.Sub(web, "web")
	if err != nil {
		log.Fatal(err)
	}
	return sub
}()

var webTemplate = func() *template.Template {
	subTemplate, err := template.New("").ParseFS(webRoot, "*.html")
	if err != nil {
		log.Fatal(err)
	}

	return subTemplate
}()

func SSEWebHandler(contextPath string, stream *eventsource.Stream) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		p := path.Join("/", strings.TrimPrefix(r.URL.Path, contextPath))
		if contextPath == "/" {
			contextPath = ""
		}

		switch p {
		case "/":
			if err := webTemplate.ExecuteTemplate(w, "index.html", map[string]string{
				"ContextPath": contextPath,
			}); err != nil {
				log.Fatal(err)
			}
		case "/sse":
			SSEHandler(stream).ServeHTTP(w, r)
		default:
			http.StripPrefix(contextPath, http.FileServer(http.FS(webRoot))).ServeHTTP(w, r)
		}
	}
}

type SSESender struct {
	stream *eventsource.Stream
}

func (s *SSESender) Send(msg string, _ bool) {
	e := ParseHTTPEvent(msg)
	d := string(codec.Json(e))
	s.stream.Broadcast(eventsource.DataEvent(d))
}

func ParseHTTPEvent(msg string) HTTPEvent {
	e := HTTPEvent{Payload: msg}

	scanner := bufio.NewScanner(strings.NewReader(msg))
	scanner.Split(replay.ScanLines)

	for scanner.Scan() {
		line := string(scanner.Bytes())
		// ### #1 REQ 127.0.0.1:54386-127.0.0.1:5003 2022-04-17T10:58:09.505447+08:00
		// ### #1 RSP 127.0.0.1:54386-127.0.0.1:5003 2022-04-17T10:58:09.505464+08:00
		// ### EOF REQ 127.0.0.1:54386-127.0.0.1:5003 2022-04-17T10:58:09.505447+08:00
		// ### EOF RSP 127.0.0.1:54386-127.0.0.1:5003 2022-04-17T10:58:09.505499+08:00
		if strings.HasPrefix(line, "###") {
			fields := strings.Fields(line)
			e.Connection = FieldsN(fields, 3)
			e.Timestamp = FieldsN(fields, 4)
			seq := FieldsN(fields, 1)
			if seq == "EOF" {
				e.EOF = true
				e.Req = FieldsN(fields, 2) == "REQ"
				e.Rsp = FieldsN(fields, 2) == "RSP"
				break
			}

			if strings.HasPrefix(seq, "#") {
				e.Seq = ss.ParseInt(seq[1:])
			}

			switch FieldsN(fields, 2) {
			case "REQ":
				e.Req = true
				e.ReqSize = man.IBytes(uint64(len(msg)))
				scanner.Scan()
				e.Method, e.Path, _ = replay.ParseRequestTitle(scanner.Bytes())
			case "RSP":
				e.Rsp = true
				e.RspSize = man.IBytes(uint64(len(msg)))
				scanner.Scan()
				e.Status, _ = util.ParseResponseTitle(scanner.Bytes())
			}

			continue
		}

		if strings.HasPrefix(line, "Host:") {
			e.Host = ss.FieldsN(line, 2)[1]
		} else if e.Rsp && strings.HasPrefix(line, "Content-Type:") {
			e.ContentType = ss.FieldsN(line, 2)[1]
		}
	}

	return e
}

func FieldsN(fields []string, seq int) string {
	return ss.If(seq < len(fields), fields[seq], "")
}

func (s *SSESender) Close() error {
	s.stream.Shutdown()
	return nil
}

var _ handler.Sender = (*SSESender)(nil)

type HTTPEvent struct {
	EOF         bool
	Req         bool
	Rsp         bool
	Seq         int
	Connection  string // // like 192.168.217.54:53933-192.168.126.182:9090
	Method      string
	Host        string
	Path        string
	ContentType string
	Status      int
	Time        string
	Size        string

	Timestamp string
	Payload   string
	ReqSize   string
	RspSize   string
}

func NewSSEStream() *eventsource.Stream {
	return eventsource.NewStream()
}

func SSEHandler(stream *eventsource.Stream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		stream.ServeHTTP(w, r)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}
