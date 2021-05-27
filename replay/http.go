package replay

import (
	"bytes"
	"net/http"
	"strings"
)

const (
	// MinRequestCount GET / HTTP/1.1\r\n.
	MinRequestCount = 16
)

// ParseRequestTitle parses an HTTP/1 request title from payload.
func ParseRequestTitle(payload []byte) (method, path string, ok bool) {
	s := SliceToString(payload)
	if len(s) < MinRequestCount {
		return "", "", false
	}
	titleLen := bytes.Index(payload, []byte("\r\n"))
	if titleLen == -1 {
		return "", "", false
	}
	if strings.Count(s[:titleLen], " ") != 2 {
		return "", "", false
	}
	method = string(Method(payload))

	if !HttpMethods[method] {
		return method, "", false
	}
	pos := strings.Index(s[len(method)+1:], " ")
	if pos == -1 {
		return method, "", false
	}
	path = s[len(method)+1 : pos]
	major, minor, ok := http.ParseHTTPVersion(s[pos+len(method)+2 : titleLen])
	return method, path, ok && major == 1 && (minor == 0 || minor == 1)
}

// Method returns HTTP method
func Method(payload []byte) []byte {
	end := bytes.IndexByte(payload, ' ')
	if end == -1 {
		return nil
	}

	return payload[:end]
}

var HttpMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodConnect: true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}
