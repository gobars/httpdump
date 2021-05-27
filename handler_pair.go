package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"github.com/bingoohuang/gg/pkg/ss"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bingoohuang/httpdump/httpport"

	"bufio"

	"github.com/google/gopacket/tcpassembly/tcpreader"
)

// ConnectionHandlerPair impl ConnectionHandler
type ConnectionHandlerPair struct {
	option *Option
	sender Sender
	wg     sync.WaitGroup
}

func (h *ConnectionHandlerPair) handle(src Endpoint, dst Endpoint, c *TCPConnection) {
	handler := &HttpTrafficHandlerPair{
		startTime: c.lastTimestamp,
		HandlerBase: HandlerBase{
			key:    &ConnectionKey{src: src, dst: dst},
			buffer: new(bytes.Buffer),
			option: h.option,
			sender: h.sender,
		},
	}
	h.wg.Add(1)
	go handler.handle(&h.wg, c)
}

func (h *ConnectionHandlerPair) finish() { h.wg.Wait() }

// HttpTrafficHandlerPair parse a http connection traffic and send to printer
type HttpTrafficHandlerPair struct {
	startTime time.Time
	endTime   time.Time

	HandlerBase
}

// read http request/response stream, and do output
func (h *HttpTrafficHandlerPair) handle(wg *sync.WaitGroup, c *TCPConnection) {
	defer wg.Done()
	defer c.requestStream.Close()
	defer c.responseStream.Close()
	// filter by args setting

	requestReader := bufio.NewReader(c.requestStream)
	defer discardAll(requestReader)
	responseReader := bufio.NewReader(c.responseStream)
	defer discardAll(responseReader)

	o := h.option

	for {
		h.buffer = new(bytes.Buffer)
		r, err := httpport.ReadRequest(requestReader)
		h.startTime = c.lastTimestamp
		if err != nil {
			if !IsEOF(err) {
				fmt.Fprintln(os.Stderr, "Error parsing HTTP requests:", err)
			}
			break
		}

		_seq := int32(0)
		seqFn := func() int32 {
			if _seq == 0 {
				_seq = reqCounter.Incr()
			}
			return _seq
		}

		filtered := o.Host != "" && !wildcardMatch(r.Host, o.Host) ||
			o.Uri != "" && !wildcardMatch(r.RequestURI, o.Uri) ||
			o.Method != "" && !strings.Contains(o.Method, r.Method)

		// if is websocket request,  by header: Upgrade: websocket
		websocket := r.Header.Get("Upgrade") == "websocket"
		expectContinue := r.Header.Get("Expect") == "100-continue"

		resp, err := httpport.ReadResponse(responseReader, nil)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			} else {
				fmt.Fprintln(os.Stderr, "Error parsing HTTP response:", err, c.clientID)
			}
			if filtered {
				discardAll(r.Body)
			} else {
				h.printRequest(r, c.requestStream.GetLastUUID(), seqFn())
				h.writeLine("")
				h.sender.Send(h.buffer.String(), true)
			}
			break
		}

		filtered = filtered || !IntSet(o.Status).Contains(resp.StatusCode)

		if filtered {
			discardAll(r.Body)
			discardAll(resp.Body)
		} else {
			h.printRequest(r, c.requestStream.GetLastUUID(), seqFn())
			h.writeLine("")
			h.endTime = c.lastTimestamp

			h.printResponse(resp, c.responseStream.GetLastUUID(), seqFn())
			h.sender.Send(h.buffer.String(), true)
		}

		if websocket {
			if resp.StatusCode == 101 && resp.Header.Get("Upgrade") == "websocket" {
				// change to handle websocket
				h.handleWebsocket(requestReader, responseReader)
				break
			}
		}

		if expectContinue {
			if resp.StatusCode == 100 {
				// read next response, the real response
				resp, err := httpport.ReadResponse(responseReader, nil)
				if err == io.EOF {
					fmt.Fprintln(os.Stderr, "Error parsing HTTP requests: unexpected end, ", err)
					break
				}
				if err == io.ErrUnexpectedEOF {
					fmt.Fprintln(os.Stderr, "Error parsing HTTP requests: unexpected end, ", err)
					// here return directly too, to avoid error when long polling c is used
					break
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, "Error parsing HTTP response:", err, c.clientID)
					break
				}
				if filtered {
					discardAll(resp.Body)
				} else {
					h.printResponse(resp, c.responseStream.GetLastUUID(), seqFn())
					h.sender.Send(h.buffer.String(), true)
				}
			} else if resp.StatusCode == 417 {

			}
		}
	}

	h.sender.Send(h.buffer.String(), true)
}

func (h *HttpTrafficHandlerPair) handleWebsocket(requestReader *bufio.Reader, responseReader *bufio.Reader) {
	//TODO: websocket
}

// print http request
func (h *HttpTrafficHandlerPair) printRequest(req *httpport.Request, uuid []byte, seq int32) {
	defer discardAll(req.Body)

	if h.option.Curl {
		h.printCurlRequest(req, uuid, seq)
	} else {
		h.printNormalRequest(req, uuid, seq)
	}
}

var blockHeaders = map[string]bool{
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Connection":        true,
	"Accept-Encoding:":  true,
}

// print http request curl command
func (h *HttpTrafficHandlerPair) printCurlRequest(req Req, uuid []byte, seq int32) {
	//TODO: expect-100 continue handle

	h.writeLine("\n### REQUEST ", h.key.Src(), "->", h.key.Dst(), h.startTime.Format(time.RFC3339Nano))
	h.writeLineFormat("curl -X %v http://%v%v \\\n", req.GetMethod(), h.key.Dst(), req.GetRequestURI())
	var reader io.ReadCloser
	deCompressed := false
	o := h.option
	if o.DumpBody != "" {
		reader = req.GetBody()
	} else {
		reader, deCompressed = tryDecompress(req.GetHeader(), req.GetBody())
	}

	if deCompressed {
		defer reader.Close()
	}
	idx := 0
	for name, values := range req.GetHeader() {
		idx++
		if blockHeaders[name] {
			continue
		}
		if deCompressed {
			if name == "Content-Encoding" {
				continue
			}
		}
		for idx, value := range values {
			if idx == len(req.GetHeader()) && idx == len(values)-1 {
				h.writeLineFormat("    -H '%v: %v'\n", name, value)
			} else {
				h.writeLineFormat("    -H '%v: %v' \\\n", name, value)
			}
		}
	}

	if req.GetContentLength() == 0 || ss.AnyOf(req.GetMethod(), "GET", "HEAD", "TRACE", "OPTIONS") {
		h.writeLine()
		return
	}

	if o.CanDump() {
		fn := bodyFileName(o.DumpBody, uuid, seq, "request", h.startTime)
		if n, err := DumpBody(reader, fn, &o.dumpNum); err != nil {
			h.writeLine("dump to file failed:", err)
		} else if n > 0 {
			h.writeLineFormat(" -d '@%v'", fn)
		}
	} else {
		br := bufio.NewReader(reader)
		// optimize for one line body
		firstLine, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			// read error
		} else if err == io.EOF && !strings.Contains(firstLine, "'") {
			h.writeLineFormat("    -d '%v'", strconv.Quote(firstLine))
		} else {
			h.writeLineFormat("    -d @- << HTTP_DUMP_BODY_EOF\n")
			h.write(firstLine)
			for {
				line, err := br.ReadString('\n')
				if err != nil && err != io.EOF {
					break
				}
				h.write(line)
				if err == io.EOF {
					h.writeLine("\nHTTP_DUMP_BODY_EOF")
					break
				}
			}
		}
	}

	h.writeLine()
}

// printNormalRequest prints http request.
func (h *HttpTrafficHandlerPair) printNormalRequest(r Req, uuid []byte, seq int32) {
	//TODO: expect-100 continue handle
	o := h.option
	if o.Level == LevelUrl {
		h.writeLine(r.GetMethod(), r.GetHost()+r.GetRequestURI())
		return
	}

	h.writeLine(fmt.Sprintf("\n### REQUEST #%d %s %s->%s %s", seq,
		uuid, h.key.Src(), h.key.Dst(), h.startTime.Format(time.RFC3339Nano)))

	h.writeLine(r.GetMethod(), r.GetRequestURI(), r.GetProto())
	h.printHeader(r.GetHeader())

	contentLength := parseContentLength(r.GetContentLength(), r.GetHeader())
	hasBody := contentLength > 0 && !ss.AnyOf(r.GetMethod(), "GET", "HEAD", "TRACE", "OPTIONS")

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, uuid, seq, "request", h.startTime)
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
func (h *HttpTrafficHandlerPair) printResponse(r Rsp, uuid []byte, seq int32) {
	defer discardAll(r.GetBody())

	o := h.option
	if !o.Resp {
		return
	}

	h.writeLine(fmt.Sprintf("\n### RESPONSE #%d %s %s->%s %s-%s cost %s", seq,
		uuid, h.key.Src(), h.key.Dst(), h.startTime.Format(time.RFC3339Nano),
		h.endTime.Format(time.RFC3339Nano), h.endTime.Sub(h.startTime).String()))
	h.writeLine(r.GetStatusLine())

	if o.Level == LevelUrl {
		return
	}

	for _, header := range r.GetRawHeaders() {
		h.writeLine(header)
	}

	contentLength := parseContentLength(r.GetContentLength(), r.GetHeader())
	hasBody := contentLength > 0 && r.GetStatusCode() != 304 && r.GetStatusCode() != 204

	if hasBody && o.CanDump() {
		fn := bodyFileName(o.DumpBody, uuid, seq, "response", h.startTime)
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

func tryDecompress(header http.Header, reader io.ReadCloser) (io.ReadCloser, bool) {
	contentEncoding := header.Get("Content-Encoding")
	var nr io.ReadCloser
	var err error
	if contentEncoding == "" {
		// do nothing
		return reader, false
	} else if strings.Contains(contentEncoding, "gzip") {
		nr, err = gzip.NewReader(reader)
		if err != nil {
			return reader, false
		}
		return nr, true
	} else if strings.Contains(contentEncoding, "deflate") {
		nr, err = zlib.NewReader(reader)
		if err != nil {
			return reader, false
		}
		return nr, true
	} else {
		return reader, false
	}
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

	// prettify json
	if mt.subType == "json" || likeJSON(body) {
		var jsonValue interface{}
		_ = json.Unmarshal([]byte(body), &jsonValue)
		prettyJSON, err := json.MarshalIndent(jsonValue, "", "    ")
		if err == nil {
			body = string(prettyJSON)
		}
	}
	h.writeLine(body)
	h.writeLine()
}

func (h *HandlerBase) printNonTextTypeBody(reader io.Reader, contentType string, isBinary bool) error {
	if h.option.Force && !isBinary {
		data, err := ioutil.ReadAll(reader)
		if err != nil {
			return err
		}
		// TODO: try to detect charset
		str := string(data)
		h.writeLine(str)
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
