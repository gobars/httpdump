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

// HasRequestTitle reports whether this payload has an HTTP/1 request title
func HasRequestTitle(payload []byte) (method string, yes bool) {
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
func HasResponseTitle(payload []byte) (code int, yes bool) {
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
