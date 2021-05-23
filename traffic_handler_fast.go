package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/bingoohuang/httpdump/httpport"
	"io"
	"os"
	"time"
)

// FastConnectionHandler impl ConnectionHandler
type FastConnectionHandler struct {
	option  *Option
	printer *Printer
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
	waitGroup.Add(2)
	go reqHandler.handleRequest(c)
	go rspHandler.handleResponse(c)
}

func (h *FastConnectionHandler) finish() {
	//h.printer.finish()
}

// fastTrafficHandler parse a http connection traffic and send to printer
type fastTrafficHandler struct {
	HandlerBase
}

// read http request/response stream, and do output
func (h *fastTrafficHandler) handleRequest(c *TCPConnection) {
	defer waitGroup.Done()
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
func (h *fastTrafficHandler) handleResponse(c *TCPConnection) {
	defer waitGroup.Done()
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
func (h *fastTrafficHandler) printRequest(req *httpport.Request, startTime time.Time, uuid []byte, seq int32) {
	if h.option.Level == "url" {
		h.writeLine(req.Method, req.Host+req.RequestURI)
		return
	}

	h.writeLine()
	h.writeLine(fmt.Sprintf("### REQUEST #%d %s %s->%s %s", seq,
		uuid, h.key.src, h.key.dst, startTime.Format(time.RFC3339Nano)))

	h.writeLine(req.Method, req.RequestURI, req.Proto)
	h.printHeader(req.Header)

	hasBody := true
	if req.ContentLength == 0 || req.Method == "GET" || req.Method == "HEAD" || req.Method == "TRACE" ||
		req.Method == "OPTIONS" {
		hasBody = false
	}

	if h.option.Level == "header" {
		if hasBody {
			h.writeLine("\n// body size:", discardAll(req.Body),
				", set [level = all] to display http body")
		}
		return
	}

	h.writeLine()

	if hasBody {
		h.printBody(req.Header, req.Body)
	}
}

// print http response
func (h *fastTrafficHandler) printResponse(resp *httpport.Response, endTime time.Time, uuid []byte, seq int32) {
	defer discardAll(resp.Body)

	if !h.option.PrintResp {
		return
	}

	if h.option.Level == "url" {
		return
	}

	h.writeLine("### RESPONSE #", seq, string(uuid), h.key.src, "<-", h.key.dst, endTime.Format(time.RFC3339Nano))

	h.writeLine(resp.StatusLine)
	for _, header := range resp.RawHeaders {
		h.writeLine(header)
	}

	hasBody := true
	if resp.ContentLength == 0 || resp.StatusCode == 304 || resp.StatusCode == 204 {
		hasBody = false
	}

	if h.option.Level == "header" {
		if hasBody {
			h.writeLine("\n// body size:", discardAll(resp.Body),
				", set [level = all] to display http body")
		}
		return
	}

	h.writeLine()
	if hasBody {
		h.printBody(resp.Header, resp.Body)
	}
}
