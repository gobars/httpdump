package handler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/gopacket/layers"

	"github.com/google/gopacket"
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
	option *Option
	sender Sender
	ctx    context.Context
}

func NewFactory(ctx context.Context, option *Option, sender Sender) tcpassembly.StreamFactory {
	return &Factory{ctx: ctx, option: option, sender: sender}
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
	key := &streamKey{net: netFlow, tcp: tcpFlow}

	reader := tcpreader.NewReaderStream()
	reader.LossErrors = true
	go f.run(key, &reader)
	return &reader
}

func (f *Factory) run(key *streamKey, reader *tcpreader.ReaderStream) {
	buf := bufio.NewReader(reader)
	if peek, _ := buf.Peek(8); string(peek[:5]) == "HTTP/" {
		f.runResponses(key, buf)
	} else if isHTTPRequestData(peek) {
		f.runRequests(key, buf)
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

func (h HttpReq) GetBody() io.ReadCloser         { return h.Body }
func (h HttpReq) GetHost() string                { return h.Host }
func (h HttpReq) GetRequestURI() string          { return h.RequestURI }
func (h HttpReq) GetPath() string                { return h.URL.Path }
func (h HttpReq) GetMethod() string              { return h.Method }
func (h HttpReq) GetProto() string               { return h.Proto }
func (h HttpReq) GetHeader() map[string][]string { return h.Header }
func (h HttpReq) GetContentLength() int64        { return h.ContentLength }

func (f *Factory) runResponses(key *streamKey, buf *bufio.Reader) {
	h := &Base{key: key, buffer: new(bytes.Buffer), option: f.option, sender: f.sender}

	for {
		// 坑警告，这里返回的req，由于body没有读取，reader流位置可能没有移动到http请求的结束
		r, err := http.ReadResponse(buf, nil)
		now := time.Now()
		if err != nil {
			h.handleError(err, now, "RSP")
			return
		}

		h.processResponse(true, &HttpRsp{Response: r}, h.option, now)
	}
}

func IsEOF(e error) bool {
	return e != nil && (errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF))
}

func (f *Factory) runRequests(key *streamKey, buf *bufio.Reader) {
	h := &Base{key: key, buffer: new(bytes.Buffer), option: f.option, sender: f.sender}

	for {
		// 坑警告，这里返回的req，由于body没有读取，reader流位置可能没有移动到http请求的结束
		r, err := http.ReadRequest(buf)
		now := time.Now()
		if err != nil {
			h.handleError(err, now, "REQ")
			return
		}

		h.processRequest(true, &HttpReq{Request: r}, h.option, now)
	}
}
