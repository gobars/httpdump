package handler

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bingoohuang/httpdump/util"

	"github.com/bingoohuang/gg/pkg/ss"
	"github.com/bingoohuang/httpdump/httpport"
	"github.com/google/gopacket/tcpassembly/tcpreader"
)

// ConnectionHandler is interface for handle tcp connection
type ConnectionHandler interface {
	handle(src, dst Endpoint, connection *TCPConnection)
	finish()
}

type Key interface {
	Src() string
	Dst() string
}

// ConnectionKey contains src and dst endpoint identify a connection
type ConnectionKey struct {
	src, dst Endpoint
}

// Src return the src ip and port
func (ck *ConnectionKey) Src() string { return ck.src.String() }

// Dst return the dst ip and port
func (ck *ConnectionKey) Dst() string { return ck.dst.String() }

type Sender interface {
	Send(msg string, countDiscards bool)
	Close() error
}

type Senders []Sender

func (ss Senders) Send(msg string, countDiscards bool) {
	for _, s := range ss {
		s.Send(msg, countDiscards)
	}
}

func (ss Senders) Close() error {
	for _, s := range ss {
		s.Close()
	}

	return nil
}

type HandlerBase struct {
	key    Key
	buffer *bytes.Buffer
	option *Option
	sender Sender
}

func (h *HandlerBase) writeFormat(f string, a ...interface{}) { fmt.Fprintf(h.buffer, f, a...) }
func (h *HandlerBase) write(a ...interface{})                 { fmt.Fprint(h.buffer, a...) }
func (h *HandlerBase) writeBytes(p []byte)                    { h.buffer.Write(p) }
func (h *HandlerBase) writeLine(a ...interface{}) {
	fmt.Fprint(h.buffer, a...)
	fmt.Fprintf(h.buffer, "\r\n")
}

func (h *HandlerBase) printHeader(header map[string][]string) {
	for name, values := range header {
		for _, value := range values {
			h.writeFormat("%s: %s\r\n", name, value)
		}
	}
}

type Req interface {
	GetBody() io.ReadCloser
	GetHost() string
	GetRequestURI() string
	GetPath() string
	GetMethod() string
	GetProto() string
	GetHeader() map[string][]string
	GetContentLength() int64
}

type Rsp interface {
	GetBody() io.ReadCloser
	GetStatusLine() string
	GetRawHeaders() []string
	GetContentLength() int64
	GetHeader() http.Header
	GetStatusCode() int
}

var (
	reqCounter = Counter{}
	rspCounter = Counter{}
)

// read http request/response stream, and do output
func (h *HandlerBase) handleRequest(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer c.requestStream.Close()

	rb := &bytes.Buffer{}
	var method string

	for p := range c.requestStream.Packets() {
		// 请求开头行解析成功，是一个新的请求
		if m, yes := util.ParseRequestTitle(p.Payload); yes {
			rb.Reset() // 清空缓冲
			method = m // 记录请求方法
		}

		rb.Write(p.Payload)

		if rb.Len() > 0 && h.option.PermitsMethod(method) && util.Http1EndHint(rb.Bytes()) {
			h.dealRequest(rb, h.option, c)
			rb.Reset()
		}
	}

	h.handleError(io.EOF, c.lastReqTimestamp, "REQ")
}

// read http request/response stream, and do output
func (h *HandlerBase) handleResponse(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer c.responseStream.Close()
	if !h.option.Resp {
		c.responseStream.DiscardAll()
		return
	}

	rb := &bytes.Buffer{}
	var lastCode int

	for p := range c.responseStream.Packets() {
		if code, yes := util.ParseResponseTitle(p.Payload); yes {
			rb.Reset() // 清空缓冲
			lastCode = code
		}

		rb.Write(p.Payload)
		if rb.Len() > 0 && h.option.PermitsCode(lastCode) && util.Http1EndHint(rb.Bytes()) {
			h.dealResponse(rb, h.option, c)
			rb.Reset()
		}
	}

	h.handleError(io.EOF, c.lastRspTimestamp, "RSP")
}

func (h *HandlerBase) dealRequest(rb *bytes.Buffer, o *Option, c *TCPConnection) error {
	h.buffer = new(bytes.Buffer)
	r, err := httpport.ReadRequest(bufio.NewReader(rb))
	if err != nil {
		h.handleError(err, c.lastReqTimestamp, "REQ")
	} else {
		h.processRequest(false, r, o, c.lastReqTimestamp)
	}

	return err
}

func (h *HandlerBase) dealResponse(rb *bytes.Buffer, o *Option, c *TCPConnection) error {
	h.buffer = new(bytes.Buffer)
	r, err := httpport.ReadResponse(bufio.NewReader(rb), nil)
	if err != nil {
		h.handleError(err, c.lastRspTimestamp, "RSP")
	} else {
		h.processResponse(false, r, o, c.lastRspTimestamp)
	}

	return err
}

func (h *HandlerBase) processRequest(discard bool, r Req, o *Option, startTime time.Time) {
	if discard {
		defer discardAll(r.GetBody())
	}

	if !o.Permits(r) {
		return
	}

	seq := reqCounter.Incr()
	h.printRequest(r, startTime, seq)
	h.sender.Send(h.buffer.String(), true)
}

func (h *HandlerBase) processResponse(discard bool, r Rsp, o *Option, endTime time.Time) {
	if discard {
		defer discardAll(r.GetBody())
	}

	if filtered := !o.Status.Contains(r.GetStatusCode()); filtered {
		return
	}

	seq := rspCounter.Incr()
	h.printResponse(r, endTime, seq)
	h.sender.Send(h.buffer.String(), true)
}

// print http request
func (h *HandlerBase) printRequest(r Req, startTime time.Time, seq int32) {
	h.writeLine(fmt.Sprintf("\n### #%d REQ %s-%s %s",
		seq, h.key.Src(), h.key.Dst(), startTime.Format(time.RFC3339Nano)))

	o := h.option
	if ss.AnyOf(o.Level, LevelUrl) {
		h.writeFormat("%s %s\r\n", r.GetMethod(), r.GetHost()+r.GetPath())
		return
	}

	h.writeFormat("%s %s %s\r\n", r.GetMethod(), r.GetRequestURI(), r.GetProto())
	header := r.GetHeader()
	contentLength := parseContentLength(r.GetContentLength(), header)
	header["Content-Length"] = []string{fmt.Sprintf("%d", contentLength)}
	h.printHeader(header)
	h.writeBytes([]byte("\r\n"))

	hasBody := contentLength != 0 && !ss.AnyOf(r.GetMethod(), "GET", "HEAD", "TRACE", "OPTIONS")

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, seq, "REQ", startTime)
		if n, err := DumpBody(r.GetBody(), fn, &o.dumpNum); err != nil {
			h.writeLine("dump to file failed:", err)
		} else if n > 0 {
			h.writeLine("\n// dump body to file:", fn, "size:", n)
		}
		return
	}

	if o.Level == LevelHeader {
		if hasBody {
			h.writeLine("\n// body size:", discardAll(r.GetBody()), ", set [level = all] to display http body")
		}
		return
	}

	if hasBody {
		h.printBody(header, r.GetBody())
	}
}

// print http response
func (h *HandlerBase) printResponse(r Rsp, endTime time.Time, seq int32) {
	defer discardAll(r.GetBody())
	h.writeLine(fmt.Sprintf("\n### #%d RSP %s-%s %s",
		seq, h.key.Src(), h.key.Dst(), endTime.Format(time.RFC3339Nano)))

	h.writeLine(r.GetStatusLine())
	o := h.option
	if !o.Resp || o.Level == LevelUrl {
		return
	}

	for _, header := range r.GetRawHeaders() {
		h.writeLine(header)
	}

	contentLength := parseContentLength(r.GetContentLength(), r.GetHeader())
	hasBody := contentLength > 0 && r.GetStatusCode() != 304 && r.GetStatusCode() != 204

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, seq, "RSP", endTime)
		if n, err := DumpBody(r.GetBody(), fn, &o.dumpNum); err != nil {
			h.writeLine("dump to file failed:", err)
		} else if n > 0 {
			h.writeLine("\n// dump body to file:", fn, "size:", n)
		}
		return
	}

	if o.Level == LevelHeader {
		if hasBody {
			h.writeLine("\n// body size:", discardAll(r.GetBody()), ", set [level = all] to display http body")
		}
		return
	}

	if hasBody {
		h.printBody(r.GetHeader(), r.GetBody())
	}
}

func parseContentLength(cl int64, header http.Header) int64 {
	contentLength := cl
	if cl >= 0 {
		return contentLength
	}

	if v := header.Get("Content-Length"); v != "" {
		contentLength, _ = strconv.ParseInt(v, 10, 64)
	}

	return contentLength
}

// print http request/response body
func (h *HandlerBase) printBody(header http.Header, reader io.ReadCloser) {
	// deal with content encoding such as gzip, deflate
	nr, decompressed := util.TryDecompress(header, reader)
	if decompressed {
		defer nr.Close()
	}

	// check mime type and charset
	contentType := header.Get("Content-Type")
	// if contentType == "" {
	// TODO: detect content type using httpport.DetectContentType()
	//}
	mimeTypeStr, charset := parseContentType(contentType)
	mt := parseMimeType(mimeTypeStr)
	isText := mt.isTextContent()
	isBinary := mt.isBinaryContent()

	if !isText {
		if err := h.printNonTextTypeBody(nr, contentType, isBinary); err != nil {
			h.writeLine("{Read content error", err, "}")
		}
		return
	}

	var body []byte
	var err error
	if charset == "" {
		// response do not set charset, try to detect
		if data, err := io.ReadAll(nr); err == nil {
			body = data
		}
	} else {
		body, err = readWithCharset(nr, charset)
	}
	if err != nil {
		h.writeLine("{Read body failed", err, "}")
		return
	}

	if l := len(body); l > 0 {
		h.writeBytes(body)
	}
}

func (h *HandlerBase) printNonTextTypeBody(reader io.Reader, contentType string, isBinary bool) error {
	if h.option.Force && !isBinary {
		data, err := ioutil.ReadAll(reader)
		if err != nil {
			return err
		}
		// TODO: try to detect charset
		h.writeLine(string(data))
		h.writeLine()
	} else {
		h.writeLine("{Non-text body, content-type:", contentType, ", len:", discardAll(reader), "}")
	}
	return nil
}

func discardAll(r io.Reader) (discarded int) {
	return tcpreader.DiscardBytesToEOF(r)
}

func bodyFileName(prefix string, seq int32, req string, t time.Time) string {
	timeStr := t.Format("20060102")
	return fmt.Sprintf("%s.%s.%d.%s", prefix, timeStr, seq, req)
}

func (h *HandlerBase) handleError(err error, t time.Time, requestOrResponse string) {
	k := h.key
	tim := t.Format(time.RFC3339Nano)
	if IsEOF(err) {
		msg := fmt.Sprintf("\n### EOF %s %s->%s %s", requestOrResponse, k.Src(), k.Dst(), tim)
		h.sender.Send(msg, false)
	} else {
		msg := fmt.Sprintf("\n### ERR %s %s->%s %s, error: %v", requestOrResponse, k.Src(), k.Dst(), tim, err)
		h.sender.Send(msg, false)
		fmt.Fprintf(os.Stderr, "error parsing HTTP %s, error: %v", requestOrResponse, err)
	}
}

// DumpBody write all data from a reader, to a file.
func DumpBody(r io.Reader, path string, u *uint32) (int64, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}

	n, err := io.Copy(f, r)
	if n <= 0 { // nothing to write, remove file
		os.Remove(path)
	} else {
		atomic.AddUint32(u, 1)
	}
	f.Close()
	return n, err
}

type Counter struct {
	counter int32
}

func (c *Counter) Incr() int32 { return atomic.AddInt32(&c.counter, 1) }
