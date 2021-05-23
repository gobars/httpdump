package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/bingoohuang/httpdump/httpport"
	"io"
	"os"
	"sync"
	"time"
)

// FastConnectionHandler impl ConnectionHandler
type FastConnectionHandler struct {
	option  *Option
	printer *Printer
	wg      sync.WaitGroup
}

func (h *FastConnectionHandler) handle(src Endpoint, dst Endpoint, c *TCPConnection) {
	key := ConnectionKey{src: src, dst: dst}
	reqHandler := &fastTrafficHandler{
		HandlerBase: HandlerBase{
			key:     key,
			buffer:  new(bytes.Buffer),
			option:  h.option,
			printer: h.printer,
		}}
	rspHandler := &fastTrafficHandler{
		HandlerBase: HandlerBase{
			key:     key,
			buffer:  new(bytes.Buffer),
			option:  h.option,
			printer: h.printer,
		}}
	h.wg.Add(2)
	go reqHandler.handleRequest(&h.wg, c)
	go rspHandler.handleResponse(&h.wg, c)
}

func (h *FastConnectionHandler) finish() { h.wg.Wait() }

// fastTrafficHandler parse a http connection traffic and send to printer
type fastTrafficHandler struct {
	HandlerBase
}

// read http request/response stream, and do output
func (h *fastTrafficHandler) handleRequest(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer c.requestStream.Close()

	requestReader := bufio.NewReader(c.requestStream)
	defer discardAll(requestReader)

	for {
		h.buffer = new(bytes.Buffer)
		req, err := httpport.ReadRequest(requestReader)
		startTime := c.lastReqTimestamp
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
			} else {
				fmt.Fprintln(os.Stderr, "Error parsing HTTP requests:", err)
			}
			break
		}
		seq := reqCounter.Incr()

		filtered := false
		if h.option.Host != "" && !wildcardMatch(req.Host, h.option.Host) {
			filtered = true
		} else if h.option.Uri != "" && !wildcardMatch(req.RequestURI, h.option.Uri) {
			filtered = true
		}

		if !filtered {
			h.printRequest(req, startTime, c.requestStream.LastUUID, seq)
			h.printer.send(h.buffer.String())
		} else {
			discardAll(req.Body)
		}
	}
}

var rspCounter = Counter{}

// read http request/response stream, and do output
func (h *fastTrafficHandler) handleResponse(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer c.responseStream.Close()

	if !h.option.PrintResp {
		discardAll(c.responseStream)
		return
	}

	responseReader := bufio.NewReader(c.responseStream)
	defer discardAll(responseReader)

	for {
		h.buffer = new(bytes.Buffer)
		resp, err := httpport.ReadResponse(responseReader, nil)
		endTime := c.lastRspTimestamp
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
			} else {
				fmt.Fprintln(os.Stderr, "Error parsing HTTP response:", err, c.clientID)
			}
			break
		}

		seq := rspCounter.Incr()
		filtered := false
		if !IntSet(h.option.Status).Contains(resp.StatusCode) {
			filtered = true
		}

		if !filtered {
			h.printResponse(resp, endTime, c.responseStream.LastUUID, seq)
			h.printer.send(h.buffer.String())
		} else {
			discardAll(resp.Body)
		}
	}
}

// print http request
func (h *fastTrafficHandler) printRequest(r *httpport.Request, startTime time.Time, uuid []byte, seq int32) {
	if h.option.Level == "url" {
		h.writeLine(r.Method, r.Host+r.RequestURI)
		return
	}

	h.writeLine(fmt.Sprintf("\n### REQUEST #%d %s %s->%s %s",
		seq, uuid, h.key.src, h.key.dst, startTime.Format(time.RFC3339Nano)))

	h.writeLine(r.Method, r.RequestURI, r.Proto)
	h.printHeader(r.Header)

	hasBody := true
	if r.ContentLength == 0 || r.Method == "GET" || r.Method == "HEAD" || r.Method == "TRACE" ||
		r.Method == "OPTIONS" {
		hasBody = false
	}

	if h.option.Level == "header" {
		if hasBody {
			h.writeLine("\n// body size:", discardAll(r.Body),
				", set [level = all] to display http body")
		}
		return
	}

	if hasBody {
		h.writeLine()
		h.printBody(r.Header, r.Body)
	}
}

// print http response
func (h *fastTrafficHandler) printResponse(r *httpport.Response, endTime time.Time, uuid []byte, seq int32) {
	defer discardAll(r.Body)

	if !h.option.PrintResp {
		return
	}

	if h.option.Level == "url" {
		return
	}

	h.writeLine(fmt.Sprintf("\n### RESPONSE #%d %s %s<-%s %s",
		seq, uuid, h.key.src, h.key.dst, endTime.Format(time.RFC3339Nano)))

	h.writeLine(r.StatusLine)
	for _, header := range r.RawHeaders {
		h.writeLine(header)
	}

	hasBody := true
	if r.ContentLength == 0 || r.StatusCode == 304 || r.StatusCode == 204 {
		hasBody = false
	}

	if h.option.Level == "header" {
		if hasBody {
			h.writeLine("\n// body size:", discardAll(r.Body),
				", set [level = all] to display http body")
		}
		return
	}

	if hasBody {
		h.writeLine()
		h.printBody(r.Header, r.Body)
	}
}
