package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/bingoohuang/gg/pkg/ss"
	"github.com/bingoohuang/httpdump/httpport"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FastConnectionHandler impl ConnectionHandler
type FastConnectionHandler struct {
	option *Option
	sender Sender
	wg     sync.WaitGroup
}

func (h *FastConnectionHandler) handle(src Endpoint, dst Endpoint, c *TCPConnection) {
	key := &ConnectionKey{src: src, dst: dst}
	reqHandler := &HandlerBase{key: key, buffer: new(bytes.Buffer), option: h.option, sender: h.sender}
	rspHandler := &HandlerBase{key: key, buffer: new(bytes.Buffer), option: h.option, sender: h.sender}
	h.wg.Add(2)
	go reqHandler.handleRequest(&h.wg, c)
	go rspHandler.handleResponse(&h.wg, c)
}

func (h *FastConnectionHandler) finish() { h.wg.Wait() }

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
			h.handleError(err, startTime)
			break
		}

		h.processRequest(r, c.requestStream.LastUUID, o, startTime)
	}
}

func (h *HandlerBase) handleError(err error, startTime time.Time) {
	if IsEOF(err) {
		h.sender.Send(fmt.Sprintf("\n### EOF   %s->%s %s",
			h.key.Src(), h.key.Dst(), startTime.Format(time.RFC3339Nano)))
	} else {
		h.sender.Send(fmt.Sprintf("\n### Err   %s->%s %s, error: %v",
			h.key.Src(), h.key.Dst(), startTime.Format(time.RFC3339Nano), err))
		fmt.Fprintln(os.Stderr, "Error parsing HTTP requests:", err)
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

func (h *HandlerBase) processRequest(r Req, uuid []byte, o *Option, startTime time.Time) {
	defer discardAll(r.GetBody())

	if filtered := o.Host != "" && !wildcardMatch(r.GetHost(), o.Host) ||
		o.Uri != "" && !wildcardMatch(r.GetRequestURI(), o.Uri) ||
		o.Method != "" && !strings.Contains(o.Method, r.GetMethod()); filtered {
		return
	}

	seq := reqCounter.Incr()
	h.printRequest(r, startTime, uuid, seq)
	h.sender.Send(h.buffer.String())
}

var rspCounter = Counter{}

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
			if err == io.EOF || err == io.ErrUnexpectedEOF {
			} else {
				fmt.Fprintln(os.Stderr, "Error parsing HTTP response:", err, c.clientID)
			}
			break
		}

		h.processResponse(r, c.responseStream.LastUUID, o, endTime)
	}
}

func (h *HandlerBase) processResponse(r Rsp, uuid []byte, o *Option, endTime time.Time) {
	defer discardAll(r.GetBody())

	if filtered := !IntSet(o.Status).Contains(r.GetStatusCode()); filtered {
		return
	}

	seq := rspCounter.Incr()
	h.printResponse(r, endTime, uuid, seq)
	h.sender.Send(h.buffer.String())
}

// print http request
func (h *HandlerBase) printRequest(r Req, startTime time.Time, uuid []byte, seq int32) {
	h.writeLine(fmt.Sprintf("\n### REQUEST #%d %s %s->%s %s",
		seq, uuid, h.key.Src(), h.key.Dst(), startTime.Format(time.RFC3339Nano)))

	o := h.option
	if ss.AnyOf(o.Level, LevelL0, LevelUrl) {
		h.writeLine(r.GetMethod(), r.GetHost()+r.GetPath())
		return
	}

	h.writeLine(r.GetMethod(), r.GetRequestURI(), r.GetProto())
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
		h.writeLine()
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
	if o.Level == LevelL0 {
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
