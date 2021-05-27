package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/bingoohuang/gg/pkg/ss"
	"github.com/bingoohuang/httpdump/httpport"
	"github.com/google/gopacket/tcpassembly/tcpreader"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

func (ck *ConnectionKey) reverse() ConnectionKey { return ConnectionKey{ck.dst, ck.src} }

// Src return the src ip and port
func (ck *ConnectionKey) Src() string { return ck.src.String() }

// Dst return the dst ip and port
func (ck *ConnectionKey) Dst() string { return ck.dst.String() }

type Sender interface {
	Send(msg string, countDiscards bool)
	Close() error
}

type HandlerBase struct {
	key    Key
	buffer *bytes.Buffer
	option *Option
	sender Sender
}

func (h *HandlerBase) writeFormat(f string, a ...interface{}) { fmt.Fprintf(h.buffer, f, a...) }
func (h *HandlerBase) write(a ...interface{})                 { fmt.Fprint(h.buffer, a...) }
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

var reqCounter = Counter{}
var rspCounter = Counter{}

// read http request/response stream, and do output
func (h *HandlerBase) handleRequest(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer c.requestStream.Close()

	rr := bufio.NewReader(c.requestStream)
	defer discardAll(rr)
	o := h.option

	for {
		h.buffer = new(bytes.Buffer)
		r, err := httpport.ReadRequest(rr)
		startTime := c.lastReqTimestamp
		if err != nil {
			h.handleError(err, startTime, "request")
			break
		}

		h.processRequest(r, c.requestStream.GetLastUUID(), o, startTime)
	}
}

// read http request/response stream, and do output
func (h *HandlerBase) handleResponse(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer c.responseStream.Close()

	o := h.option
	if !o.Resp {
		discardAll(c.responseStream)
		return
	}

	rr := bufio.NewReader(c.responseStream)
	defer discardAll(rr)

	for {
		h.buffer = new(bytes.Buffer)
		r, err := httpport.ReadResponse(rr, nil)
		endTime := c.lastRspTimestamp
		if err != nil {
			h.handleError(err, endTime, "response")
			break
		}

		h.processResponse(r, c.responseStream.GetLastUUID(), o, endTime)
	}
}

func (h *HandlerBase) processRequest(r Req, uuid []byte, o *Option, startTime time.Time) {
	defer discardAll(r.GetBody())

	if filtered := o.Host != "" && !wildcardMatch(r.GetHost(), o.Host) ||
		o.Uri != "" && !wildcardMatch(r.GetRequestURI(), o.Uri) ||
		o.Method != "" && !strings.Contains(o.Method, r.GetMethod()); filtered {
		return
	}

	seq := reqCounter.Incr()
	h.printRequest(r, startTime, uuid, seq)
	h.sender.Send(h.buffer.String(), true)
}

func (h *HandlerBase) processResponse(r Rsp, uuid []byte, o *Option, endTime time.Time) {
	defer discardAll(r.GetBody())

	if filtered := !o.Status.Contains(r.GetStatusCode()); filtered {
		return
	}

	seq := rspCounter.Incr()
	h.printResponse(r, endTime, uuid, seq)
	h.sender.Send(h.buffer.String(), true)
}

// print http request
func (h *HandlerBase) printRequest(r Req, startTime time.Time, uuid []byte, seq int32) {
	h.writeLine(fmt.Sprintf("\n### REQUEST #%d %s %s->%s %s",
		seq, uuid, h.key.Src(), h.key.Dst(), startTime.Format(time.RFC3339Nano)))

	o := h.option
	if ss.AnyOf(o.Level, LevelL1, LevelUrl) {
		h.writeFormat("%s %s\r\n", r.GetMethod(), r.GetHost()+r.GetPath())
		return
	}

	h.writeFormat("%s %s %s\r\n", r.GetMethod(), r.GetRequestURI(), r.GetProto())
	h.printHeader(r.GetHeader())

	contentLength := parseContentLength(r.GetContentLength(), r.GetHeader())
	hasBody := contentLength != 0 && !ss.AnyOf(r.GetMethod(), "GET", "HEAD", "TRACE", "OPTIONS")

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, uuid, seq, "request", startTime)
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
		h.writeFormat("\r\n")
		h.printBody(r.GetHeader(), r.GetBody())
	}
}

// print http response
func (h *HandlerBase) printResponse(r Rsp, endTime time.Time, uuid []byte, seq int32) {
	defer discardAll(r.GetBody())

	o := h.option
	if !o.Resp || o.Level == LevelUrl {
		return
	}

	h.writeLine(fmt.Sprintf("\n### RESPONSE #%d %s %s<-%s %s",
		seq, uuid, h.key.Src(), h.key.Dst(), endTime.Format(time.RFC3339Nano)))

	h.writeLine(r.GetStatusLine())
	if o.Level == LevelL1 {
		return
	}

	for _, header := range r.GetRawHeaders() {
		h.writeLine(header)
	}

	contentLength := parseContentLength(r.GetContentLength(), r.GetHeader())
	hasBody := contentLength > 0 && r.GetStatusCode() != 304 && r.GetStatusCode() != 204

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, uuid, seq, "response", endTime)
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
		h.writeLine()
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
	nr, decompressed := tryDecompress(header, reader)
	if decompressed {
		defer nr.Close()
	}

	// check mime type and charset
	contentType := header.Get("Content-Type")
	if contentType == "" {
		// TODO: detect content type using httpport.DetectContentType()
	}
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

	var body string
	var err error
	if charset == "" {
		// response do not set charset, try to detect
		if data, err := io.ReadAll(nr); err == nil {
			// TODO: try to detect charset
			body = string(data)
		}
	} else {
		body, err = readToStringWithCharset(nr, charset)
	}
	if err != nil {
		h.writeLine("{Read body failed", err, "}")
		return
	}

	h.write(body)
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

func bodyFileName(prefix string, uuid []byte, seq int32, req string, t time.Time) string {
	timeStr := t.Format("20060102")
	// parse id from uuid like id:a4af2382c0a6df1c6fb4280a,Seq:1874077706,Ack:2080684195
	if f := bytes.IndexRune(uuid, ':'); f >= 0 {
		if c := bytes.IndexRune(uuid[f:], ','); c >= 0 {
			uuid = uuid[f+1 : f+c]
		}
	}
	return fmt.Sprintf("%s.%s.%d.%s.%s", prefix, timeStr, seq, uuid, req)
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
