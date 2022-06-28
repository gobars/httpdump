package handler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bingoohuang/gg/pkg/ginx"
	"github.com/bingoohuang/gg/pkg/osx"

	"github.com/bingoohuang/gg/pkg/iox"
	"go.uber.org/multierr"

	"github.com/bingoohuang/httpdump/util"

	"github.com/bingoohuang/gg/pkg/ss"
	"github.com/bingoohuang/httpdump/httpport"
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
	io.Closer
}

type Senders []Sender

func (ss Senders) Send(msg string, countDiscards bool) {
	for _, s := range ss {
		s.Send(msg, countDiscards)
	}
}

func (ss Senders) Close() (err error) {
	for _, s := range ss {
		err = multierr.Append(err, s.Close())
	}

	return err
}

func IsUsingJSON() bool {
	return ss.AnyOfFold(os.Getenv("PRINT_JSON"), "y", "1", "yes", "on")
}

type Base struct {
	context.Context

	key       Key
	reqBuffer bytes.Buffer
	rspBuffer bytes.Buffer
	option    *Option
	sender    Sender

	reqCounter Counter
	rspCounter Counter

	usingJSON bool
}

func writeFormat(b *bytes.Buffer, f string, a ...interface{}) { _, _ = fmt.Fprintf(b, f, a...) }
func writeBytes(b *bytes.Buffer, p []byte)                    { b.Write(p) }
func writeLine(b *bytes.Buffer, a ...interface{}) {
	_, _ = fmt.Fprint(b, a...)
	_, _ = fmt.Fprintf(b, "\r\n")
}

func printHeader(b *bytes.Buffer, header map[string][]string) {
	for name, values := range header {
		for _, value := range values {
			writeFormat(b, "%s: %s\r\n", name, value)
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
	GetHeader() http.Header
	GetContentLength() int64
}

type ReqBean struct {
	Seq        int32
	Src, Dest  string
	Timestamp  string
	RequestURI string
	Method     string
	Host       string
	Header     http.Header
	Body       string `json:",clearQuotes"`
}

var MaxBodySize = osx.EnvSize("MAX_BODY_SIZE", 4096)

func ReadBody(h interface {
	GetBody() io.ReadCloser
	GetHeader() http.Header
	GetContentLength() int64
},
) string {
	_, data, _ := ReadTextBody(h.GetHeader(), h.GetBody(), int64(MaxBodySize))
	return string(data)
}

func ReqToJSON(ctx context.Context, h Req, seq int32, src, dest, timestamp string) ([]byte, error) {
	bean := ReqBean{
		Seq:        seq,
		Src:        src,
		Dest:       dest,
		Timestamp:  timestamp,
		Host:       h.GetHost(),
		RequestURI: h.GetRequestURI(),
		Method:     h.GetMethod(),
		Header:     h.GetHeader(),
		Body:       ReadBody(h),
	}

	return ginx.JsoniConfig.Marshal(ctx, bean)
}

type RspBean struct {
	Seq       int32
	Src, Dest string
	Timestamp string

	Header     http.Header
	Body       string `json:",clearQuotes"`
	StatusCode int
}

func RspToJSON(ctx context.Context, h Rsp, seq int32, src, dest, timestamp string) ([]byte, error) {
	bean := RspBean{
		Seq:        seq,
		Src:        src,
		Dest:       dest,
		Timestamp:  timestamp,
		StatusCode: h.GetStatusCode(),
		Header:     h.GetHeader(),
		Body:       ReadBody(h),
	}
	return ginx.JsoniConfig.Marshal(ctx, bean)
}

type Rsp interface {
	GetBody() io.ReadCloser
	GetStatusLine() string
	GetRawHeaders() []string
	GetContentLength() int64
	GetHeader() http.Header
	GetStatusCode() int
}

// read http request/response stream, and do output
func (h *Base) handleRequest(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer iox.Close(c.requestStream)

	rb := &bytes.Buffer{}
	var method string

	for p := range c.requestStream.Packets() {
		// 请求开头行解析成功，是一个新的请求
		m, yes := util.ParseRequestTitle(p.Payload)
		// log.Printf("ParseRequestTitle: method: %s yes: %t payload: %q", m, yes, string(p.Payload))
		if yes {
			rb.Reset() // 清空缓冲
			method = m // 记录请求方法
		}

		rb.Write(p.Payload)

		// permitsMethod := h.option.PermitsMethod(method)
		// http1EndHint := util.Http1EndHint(rb.Bytes())
		// log.Printf("rb.Len(): %d, permitsMethod: %t, http1EndHint: %t", rb.Len(), permitsMethod, http1EndHint)
		if rb.Len() > 0 && h.option.PermitsMethod(method) && util.Http1EndHint(rb.Bytes()) && h.LimitAllow() {
			h.dealRequest(rb, h.option, c)
			rb.Reset()
		}

		if h.option.ReachedN() {
			return
		}
	}

	if rb.Len() > 0 && h.option.PermitsMethod(method) && h.LimitAllow() {
		h.dealRequest(rb, h.option, c)
	}

	h.handleError(io.EOF, c.lastReqTimestamp, TagRequest)
}

// read http request/response stream, and do output
func (h *Base) handleResponse(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer iox.Close(c.responseStream)

	rb := &bytes.Buffer{}
	var lastCode int

	for p := range c.responseStream.Packets() {
		if code, yes := util.ParseResponseTitle(p.Payload); yes {
			rb.Reset() // 清空缓冲
			lastCode = code
		}

		rb.Write(p.Payload)

		if rb.Len() > 0 && h.option.PermitsCode(lastCode) && util.Http1EndHint(rb.Bytes()) && h.LimitAllow() {
			h.dealResponse(rb, h.option, c)
			rb.Reset()
		}

		if h.option.ReachedN() {
			return
		}
	}

	if rb.Len() > 0 && h.option.PermitsCode(lastCode) && h.LimitAllow() {
		h.dealResponse(rb, h.option, c)
	}

	h.handleError(io.EOF, c.lastRspTimestamp, TagResponse)
}

func (h *Base) dealRequest(rb *bytes.Buffer, o *Option, c *TCPConnection) {
	h.reqBuffer.Reset()
	if r, err := httpport.ReadRequest(bufio.NewReader(rb)); err != nil {
		h.handleError(err, c.lastReqTimestamp, TagRequest)
	} else {
		h.processRequest(false, r, o, c.lastReqTimestamp)
	}
}

func (h *Base) dealResponse(rb *bytes.Buffer, o *Option, c *TCPConnection) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("E! recover: %+v", err)
		}
	}()

	h.rspBuffer.Reset()
	if r, err := httpport.ReadResponse(bufio.NewReader(rb), nil); err != nil {
		h.handleError(err, c.lastRspTimestamp, TagResponse)
	} else {
		h.processResponse(false, r, o, c.lastRspTimestamp)
	}
}

func (h *Base) processRequest(discard bool, r Req, o *Option, startTime time.Time) {
	seq := h.reqCounter.Incr()

	if discard {
		defer discardAll(r.GetBody())
	}

	if o.PermitsReq(r) {
		if h.usingJSON {
			data, err := ReqToJSON(h.Context, r, seq, h.key.Src(), h.key.Dst(), startTime.Format(time.RFC3339Nano))
			if err != nil {
				log.Printf("req to JSON  failed: %v", err)
			}

			h.sender.Send(string(data)+"\n", true)
		} else {
			h.printRequest(r, startTime, seq)
			h.sender.Send(h.reqBuffer.String(), true)
		}
	}
}

func (h *Base) processResponse(discard bool, r Rsp, o *Option, endTime time.Time) {
	seq := h.rspCounter.Incr()
	if discard {
		defer discardAll(r.GetBody())
	}

	if o.PermitRatio() {
		return
	}

	if h.usingJSON {
		data, err := RspToJSON(h.Context, r, seq, h.key.Src(), h.key.Dst(), endTime.Format(time.RFC3339Nano))
		if err != nil {
			log.Printf("req to JSON  failed: %v", err)
		}

		h.sender.Send(string(data)+"\n", true)
	} else {
		h.printResponse(r, endTime, seq)
		h.sender.Send(h.rspBuffer.String(), true)
	}
}

// print http request
func (h *Base) printRequest(r Req, startTime time.Time, seq int32) {
	b := &h.reqBuffer
	writeLine(b, fmt.Sprintf("\n### #%d REQ %s-%s %s",
		seq, h.key.Src(), h.key.Dst(), startTime.Format(time.RFC3339Nano)))

	o := h.option
	if ss.AnyOf(o.Level, LevelUrl) {
		writeFormat(b, "%s %s\r\n", r.GetMethod(), r.GetHost()+r.GetPath())
		return
	}

	writeFormat(b, "%s %s %s\r\n", r.GetMethod(), r.GetRequestURI(), r.GetProto())
	header := r.GetHeader()
	contentLength := parseContentLength(r.GetContentLength(), header)
	header["Content-Length"] = []string{fmt.Sprintf("%d", contentLength)}
	printHeader(b, header)
	writeBytes(b, []byte("\r\n"))

	hasBody := contentLength != 0 && !ss.AnyOf(r.GetMethod(), "GET", "HEAD", "TRACE", "OPTIONS")

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, seq, "REQ", startTime)
		if n, err := DumpBody(r.GetBody(), fn, &o.dumpNum); err != nil {
			writeLine(b, "dump to file failed:", err)
		} else if n > 0 {
			writeLine(b, "\n// dump body to file:", fn, "size:", n)
		}
		return
	}

	if o.Level == LevelHeader {
		if hasBody {
			writeLine(b, "\n// body size:", discardAll(r.GetBody()), ", set [level = all] to display http body")
		}
		return
	}

	if hasBody {
		h.printBody(b, header, r.GetBody())
	}
}

// print http response
func (h *Base) printResponse(r Rsp, endTime time.Time, seq int32) {
	b := &h.rspBuffer

	writeLine(b, fmt.Sprintf("\n### #%d RSP %s-%s %s",
		seq, h.key.Src(), h.key.Dst(), endTime.Format(time.RFC3339Nano)))

	writeLine(b, r.GetStatusLine())
	o := h.option
	if o.Level == LevelUrl {
		return
	}

	for _, header := range r.GetRawHeaders() {
		writeLine(b, header)
	}
	writeBytes(b, []byte("\r\n"))

	contentLength := parseContentLength(r.GetContentLength(), r.GetHeader())
	hasBody := contentLength > 0 && r.GetStatusCode() != 304 && r.GetStatusCode() != 204

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, seq, "RSP", endTime)
		if n, err := DumpBody(r.GetBody(), fn, &o.dumpNum); err != nil {
			writeLine(b, "dump to file failed:", err)
		} else if n > 0 {
			writeLine(b, "\n// dump body to file:", fn, "size:", n)
		}
		return
	}

	if o.Level == LevelHeader {
		if hasBody {
			writeLine(b, "\n// body size:", discardAll(r.GetBody()), ", set [level = all] to display http body")
		}
		return
	}

	if hasBody {
		h.printBody(b, r.GetHeader(), r.GetBody())
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

// ReadTextBody read http request/response body if it is text.
func ReadTextBody(header http.Header, reader io.ReadCloser, limitSize int64) (MimeType, []byte, bool) {
	// deal with content encoding such as gzip, deflate
	nr, decompressed := util.TryDecompress(header, reader)
	if decompressed {
		defer iox.Close(nr)
	}

	// check mime type and charset
	contentType := header.Get("Content-Type")
	mimeTypeStr, charset := ParseContentType(contentType)
	mt := ParseMimeType(mimeTypeStr)

	if !mt.isTextContent() {
		return mt, []byte("(binary)"), false
	}

	var (
		err  error
		body []byte
	)

	if limitSize > 0 {
		nr = io.NopCloser(io.LimitReader(nr, limitSize))
	}

	if charset == "" {
		body, err = io.ReadAll(nr)
	} else {
		body, err = ReadWithCharset(nr, charset)
	}

	if err != nil {
		log.Printf("read body failed: %v", err)
		return mt, []byte("(failed)"), false
	}

	return mt, body, true
}

// print http request/response body
func (h *Base) printBody(b *bytes.Buffer, header http.Header, reader io.ReadCloser) {
	// deal with content encoding such as gzip, deflate
	nr, decompressed := util.TryDecompress(header, reader)
	if decompressed {
		defer iox.Close(nr)
	}

	// check mime type and charset
	contentType := header.Get("Content-Type")
	mimeTypeStr, charset := ParseContentType(contentType)
	if mt := ParseMimeType(mimeTypeStr); !mt.isTextContent() {
		if err := h.printNonTextTypeBody(b, nr, contentType, mt.isBinaryContent()); err != nil {
			writeLine(b, "{Read content error", err, "}")
		}
		return
	}

	var (
		err  error
		body []byte
	)

	if charset == "" {
		body, err = io.ReadAll(nr)
	} else {
		body, err = ReadWithCharset(nr, charset)
	}
	if err != nil {
		writeLine(b, "{Read body failed", err, "}")
		return
	}

	if l := len(body); l > 0 {
		writeBytes(b, body)
	}
}

func (h *Base) printNonTextTypeBody(b *bytes.Buffer, reader io.Reader, contentType string, isBinary bool) error {
	if h.option.Force || !isBinary {
		data, err := ioutil.ReadAll(reader)
		if err != nil {
			return err
		}
		// TODO: try to detect charset
		writeLine(b, string(data))
		writeLine(b)
	} else {
		writeLine(b, "{Non-text body, content-type:", contentType, ", len:", discardAll(reader), "}")
	}
	return nil
}

func discardAll(r io.Reader) int64 {
	n, _ := io.Copy(io.Discard, r)
	return n
}

func bodyFileName(prefix string, seq int32, req string, t time.Time) string {
	return fmt.Sprintf("%s.%s.%d.%s", prefix, t.Format("20060102"), seq, req)
}

type Tag string

const (
	TagRequest  Tag = "REQ"
	TagResponse Tag = "RSP"
)

func isEOF(e error) bool {
	return e != nil && (errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF))
}

func (h *Base) handleError(err error, t time.Time, tag Tag) {
	if h.usingJSON {
		return
	}

	var seq int32
	if tag == TagRequest {
		seq = h.reqCounter.Get()
	} else {
		seq = h.rspCounter.Get()
	}
	k := h.key
	tim := t.Format(time.RFC3339Nano)
	if isEOF(err) {
		if h.option.Eof {
			msg := fmt.Sprintf("\n### EOF#%d %s %s-%s %s", seq, tag, k.Src(), k.Dst(), tim)
			h.sender.Send(msg, false)
		}
	} else {
		msg := fmt.Sprintf("\n### ERR#%d %s %s-%s %s, error: %v", seq, tag, k.Src(), k.Dst(), tim, err)
		h.sender.Send(msg, false)
		_, _ = fmt.Fprintf(os.Stderr, "error parsing HTTP %s, error: %v", tag, err)
	}
}

func (h *Base) LimitAllow() bool {
	l := h.option.RateLimiter
	return l == nil || l.Allow()
}

// DumpBody write all data from a reader, to a file.
func DumpBody(r io.Reader, path string, u *uint32) (int64, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}

	n, err := io.Copy(f, r)
	if n <= 0 { // nothing to write, remove file
		_ = os.Remove(path)
	} else {
		atomic.AddUint32(u, 1)
	}
	iox.Close(f)
	return n, err
}

type Counter struct {
	counter int32
}

func (c *Counter) Incr() int32 { return atomic.AddInt32(&c.counter, 1) }
func (c *Counter) Get() int32  { return atomic.LoadInt32(&c.counter) }
