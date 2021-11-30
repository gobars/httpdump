package util

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"strings"
	"unsafe"
)

func Http1StartHint(payload []byte) (isRequest, isResponse bool) {
	if HasRequestTitle(payload) {
		return true, false
	}
	if HasResponseTitle(payload) {
		return false, true
	}

	// No request or response detected
	return false, false
}

func Http1EndHint(payload []byte) bool {
	return HasFullPayload(nil, payload)
}

// HasRequestTitle reports whether this payload has an HTTP/1 request title
func HasRequestTitle(payload []byte) bool {
	_, yes := ParseRequestTitle(payload)
	return yes
}

// ParseRequestTitle reports whether this payload has an HTTP/1 request title
func ParseRequestTitle(payload []byte) (method string, yes bool) {
	s := SliceToString(payload)
	if len(s) < MinRequestCount {
		return "", false
	}
	titleLen := bytes.Index(payload, CRLF)
	if titleLen == -1 {
		return "", false
	}
	if strings.Count(s[:titleLen], " ") != 2 {
		return "", false
	}
	method = string(Method(payload))
	if !Methods[method] {
		return "", false
	}
	path := strings.Index(s[len(method)+1:], " ")
	if path == -1 {
		return "", false
	}
	major, minor, ok := http.ParseHTTPVersion(s[path+len(method)+2 : titleLen])
	if !(ok && major == 1 && (minor == 0 || minor == 1)) {
		return "", false
	}

	return method, true
}

// HasResponseTitle reports whether this payload has an HTTP/1 response title
func HasResponseTitle(payload []byte) bool {
	_, yes := ParseResponseTitle(payload)
	return yes
}

// ParseResponseTitle reports whether this payload has an HTTP/1 response title
func ParseResponseTitle(payload []byte) (code int, yes bool) {
	s := SliceToString(payload)
	if len(s) < MinResponseCount {
		return 0, false
	}
	titleLen := bytes.Index(payload, CRLF)
	if titleLen == -1 {
		return 0, false
	}
	major, minor, ok := http.ParseHTTPVersion(s[0:VersionLen])
	if !(ok && major == 1 && (minor == 0 || minor == 1)) {
		return 0, false
	}
	if s[VersionLen] != ' ' {
		return 0, false
	}
	status, ok := atoI(payload[VersionLen+1:VersionLen+4], 10)
	if !ok {
		return 0, false
	}
	// only validate status codes mentioned in rfc2616.
	if http.StatusText(status) == "" {
		return 0, false
	}
	// handle cases from #875
	if !(payload[VersionLen+4] == ' ' || payload[VersionLen+4] == '\r') {
		return 0, false
	}

	return status, true
}

// SliceToString preferred for large body payload (zero allocation and faster)
func SliceToString(buf []byte) string {
	return *(*string)(unsafe.Pointer(&buf))
}

const (
	//MinRequestCount GET / HTTP/1.1\r\n
	MinRequestCount = 16
	// MinResponseCount HTTP/1.1 200\r\n
	MinResponseCount = 14
	// VersionLen HTTP/1.1
	VersionLen = 8
)

// CRLF In HTTP newline defined by 2 bytes (for both windows and *nix support)
var CRLF = []byte("\r\n")

// EmptyLine acts as separator: end of Headers or Body (in some cases)
var EmptyLine = []byte("\r\n\r\n")

// MIMEHeadersEndPos finds end of the Headers section, which should end with empty line.
func MIMEHeadersEndPos(payload []byte) int {
	pos := bytes.Index(payload, EmptyLine)
	if pos < 0 {
		return -1
	}
	return pos + 4
}

// MIMEHeadersStartPos finds start of Headers section
// It just finds position of second line (first contains location and method).
func MIMEHeadersStartPos(payload []byte) int {
	pos := bytes.Index(payload, CRLF)
	if pos < 0 {
		return -1
	}
	return pos + 2 // Find first line end
}

// Method returns HTTP method
func Method(payload []byte) []byte {
	end := bytes.IndexByte(payload, ' ')
	if end == -1 {
		return nil
	}

	return payload[:end]
}

// Methods holds the http methods ordered in ascending order
var Methods = map[string]bool{
	http.MethodConnect: true, http.MethodDelete: true, http.MethodGet: true,
	http.MethodHead: true, http.MethodOptions: true, http.MethodPatch: true,
	http.MethodPost: true, http.MethodPut: true, http.MethodTrace: true,
}

// ProtocolStateSetter is an interface used to provide protocol state for future use
type ProtocolStateSetter interface {
	SetProtocolState(interface{})
	ProtocolState() interface{}
}

type httpProto struct {
	body         int // body index
	headerStart  int
	headerParsed bool // we checked necessary headers
	hasFullBody  bool // all chunks has been parsed
	isChunked    bool // Transfer-Encoding: chunked
	bodyLen      int  // Content-Length's value
	hasTrailer   bool // Trailer header?
}

// HasFullPayload checks if this message has full or valid payloads and returns true.
// Message param is optional but recommended on cases where 'data' is storing
// partial-to-full stream of bytes(packets).
func HasFullPayload(state *httpProto, payload []byte) bool {
	if state == nil {
		state = new(httpProto)
	}
	if state.headerStart < 1 {
		state.headerStart = MIMEHeadersStartPos(payload)
		if state.headerStart < 0 {
			return false
		}
	}

	if state.body < 1 {
		var pos int
		endPos := MIMEHeadersEndPos(payload)
		if endPos < 0 {
			pos += len(payload)
		} else {
			pos += endPos
		}

		if endPos > 0 {
			state.body = pos
		}
	}

	if !state.headerParsed {
		chunked := Header(payload, []byte("Transfer-Encoding"))

		if len(chunked) > 0 && bytes.Index(payload, []byte("chunked")) > 0 {
			state.isChunked = true
			// trailers are generally not allowed in non-chunks body
			state.hasTrailer = len(Header(payload, []byte("Trailer"))) > 0
		} else {
			contentLen := Header(payload, []byte("Content-Length"))
			state.bodyLen, _ = atoI(contentLen, 10)
		}

		pos := len(payload)
		if state.bodyLen > 0 || pos >= state.body {
			state.headerParsed = true
		}
	}

	bodyLen := len(payload)
	bodyLen -= state.body

	if state.isChunked {
		// check chunks
		if bodyLen < 1 {
			return false
		}

		// check trailer headers
		if state.hasTrailer {
			if bytes.HasSuffix(payload, []byte("\r\n\r\n")) {
				return true
			}
		} else {
			if bytes.HasSuffix(payload, []byte("0\r\n\r\n")) {
				state.hasFullBody = true
				return true
			}
		}

		return false
	}

	// check for content-length header
	return state.bodyLen == bodyLen
}

// Header returns header value, if header not found, value will be blank
func Header(payload, name []byte) []byte {
	val, _, _, _, _ := header(payload, name)

	return val
}

// HasTitle reports if this payload has an http/1 title
func HasTitle(payload []byte) bool {
	return HasRequestTitle(payload) || HasResponseTitle(payload)
}

// header return value and positions of header/value start/end.
// If not found, value will be blank, and headerStart will be -1
// Do not support multi-line headers.
func header(payload []byte, name []byte) (value []byte, headerStart, headerEnd, valueStart, valueEnd int) {
	if HasTitle(payload) {
		headerStart = MIMEHeadersStartPos(payload)
		if headerStart < 0 {
			return
		}
	} else {
		headerStart = 0
	}

	var colonIndex int
	for headerStart < len(payload) {
		headerEnd = bytes.IndexByte(payload[headerStart:], '\n')
		if headerEnd == -1 {
			break
		}
		headerEnd += headerStart
		colonIndex = bytes.IndexByte(payload[headerStart:headerEnd], ':')
		if colonIndex == -1 {
			break
		}
		colonIndex += headerStart
		if bytes.EqualFold(payload[headerStart:colonIndex], name) {
			valueStart = colonIndex + 1
			valueEnd = headerEnd - 2
			break
		}
		headerStart = headerEnd + 1 // move to the next header
	}
	if valueStart == 0 {
		headerStart = -1
		headerEnd = -1
		valueEnd = -1
		valueStart = -1
		return
	}

	// ignore empty space after ':'
	for valueStart < valueEnd {
		if payload[valueStart] < 0x21 {
			valueStart++
		} else {
			break
		}
	}

	// ignore empty space at end of header value
	for valueEnd > valueStart {
		if payload[valueEnd] < 0x21 {
			valueEnd--
		} else {
			break
		}
	}
	value = payload[valueStart : valueEnd+1]

	return
}

// this works with positive integers
func atoI(s []byte, base int) (num int, ok bool) {
	var v int
	ok = true
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			ok = false
			break
		}
		v = int(hexTable[s[i]])
		if v >= base || (v == 0 && s[i] != '0') {
			ok = false
			break
		}
		num = (num * base) + v
	}
	return
}

var hexTable = [128]byte{
	'0': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8, '9': 9, 'A': 10, 'a': 10,
	'B': 11, 'b': 11, 'C': 12, 'c': 12, 'D': 13, 'd': 13, 'E': 14, 'e': 14, 'F': 15, 'f': 15,
}

func TryDecompress(header http.Header, reader io.ReadCloser) (io.ReadCloser, bool) {
	contentEncoding := header.Get("Content-Encoding")
	var nr io.ReadCloser
	var err error
	if contentEncoding == "" {
		// do nothing
		return reader, false
	}

	if strings.Contains(contentEncoding, "gzip") {
		nr, err = gzip.NewReader(reader)
		if err != nil {
			return reader, false
		}
		return nr, true
	}

	if strings.Contains(contentEncoding, "deflate") {
		nr, err = zlib.NewReader(reader)
		if err != nil {
			return reader, false
		}
		return nr, true
	}

	return reader, false
}
