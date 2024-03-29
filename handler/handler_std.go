package handler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/tcpassembly"
	"github.com/google/gopacket/tcpassembly/tcpreader"
)

type TcpStdAssembler struct {
	*tcpassembly.Assembler
}

func (r *TcpStdAssembler) FinishAll() {
	r.Assembler.FlushAll()
}
func (r *TcpStdAssembler) FlushOlderThan(time time.Time) { r.Assembler.FlushOlderThan(time) }

func (r *TcpStdAssembler) Assemble(flow gopacket.Flow, tcp *layers.TCP, timestamp time.Time) {
	r.Assembler.AssembleWithTimestamp(flow, tcp, timestamp)
}

type Factory struct {
	context.Context

	option *Option
	sender Sender
}

func NewFactory(ctx context.Context, option *Option, sender Sender) tcpassembly.StreamFactory {
	return &Factory{Context: ctx, option: option, sender: sender}
}

type streamKey struct {
	net, tcp gopacket.Flow
}

func (k streamKey) Src() string { return fmt.Sprintf("%v:%v", k.net.Src(), k.tcp.Src()) }
func (k streamKey) Dst() string { return fmt.Sprintf("%v:%v", k.net.Dst(), k.tcp.Dst()) }

func (k streamKey) String() string { // like 192.168.217.54:53933-192.168.126.182:9090
	return fmt.Sprintf("%v:%v-%v:%v", k.net.Src(), k.tcp.Src(), k.net.Dst(), k.tcp.Dst())
}

var _ Key = (*streamKey)(nil)

func (f *Factory) New(netFlow, tcpFlow gopacket.Flow) tcpassembly.Stream {
	h := NewBase(f.Context, &streamKey{net: netFlow, tcp: tcpFlow}, f.option, f.sender)
	reader := tcpreader.NewReaderStream()
	reader.LossErrors = true
	go f.run(h, &reader)
	return &reader
}

func (f *Factory) run(b *Base, reader *tcpreader.ReaderStream) {
	buf := bufio.NewReader(reader)
	if peek, _ := buf.Peek(8); string(peek[:5]) == "HTTP/" {
		if b.option.Resp > 0 {
			f.runResponses(b, buf)
		}
	} else if isHTTPRequestData(peek) {
		f.runRequests(b, buf)
	}

	_, _ = io.Copy(io.Discard, reader)
}

type HttpRsp struct {
	*http.Response
}

func (h HttpRsp) GetBody() io.ReadCloser  { return h.Response.Body }
func (h HttpRsp) GetStatusLine() string   { return h.Response.Status }
func (h HttpRsp) GetRawHeaders() []string { return MapKeys(h.Response.Header) }
func (h HttpRsp) GetContentLength() int64 { return h.Response.ContentLength }
func (h HttpRsp) GetHeader() http.Header  { return h.Response.Header }
func (h HttpRsp) GetStatusCode() int      { return h.Response.StatusCode }

func MapKeys(header http.Header) []string {
	keys := make([]string, 0, len(header))
	for k, v := range header {
		for _, w := range v {
			keys = append(keys, k+": "+w)
		}
	}

	return keys
}

type HttpReq struct {
	*http.Request
}

func (h HttpReq) GetBody() io.ReadCloser  { return h.Body }
func (h HttpReq) GetHost() string         { return h.Host }
func (h HttpReq) GetRequestURI() string   { return h.RequestURI }
func (h HttpReq) GetPath() string         { return h.URL.Path }
func (h HttpReq) GetMethod() string       { return h.Method }
func (h HttpReq) GetProto() string        { return h.Proto }
func (h HttpReq) GetHeader() http.Header  { return h.Header }
func (h HttpReq) GetContentLength() int64 { return h.ContentLength }

func (f *Factory) runResponses(h *Base, buf *bufio.Reader) {
	for {
		// 坑警告，这里返回的req，由于body没有读取，reader流位置可能没有移动到http请求的结束
		r, err := http.ReadResponse(buf, nil)
		now := time.Now()
		if err != nil {
			h.handleError(err, now, TagResponse)
			return
		}

		h.processResponse(true, &HttpRsp{Response: r}, h.option, now)
	}
}

func (f *Factory) runRequests(h *Base, buf *bufio.Reader) {
	for {
		// 坑警告，这里返回的req，由于body没有读取，reader流位置可能没有移动到http请求的结束
		r, err := http.ReadRequest(buf)
		now := time.Now()
		if err != nil {
			h.handleError(err, now, TagRequest)
			return
		}

		h.processRequest(true, &HttpReq{Request: r}, h.option, now)
	}
}
