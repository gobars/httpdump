package util

import (
	"bytes"
	"testing"
)

func TestHeader(t *testing.T) {
	var payload, val []byte
	var headerStart int

	// Value with space at start
	payload = []byte("POST /post HTTP/1.1\r\nContent-Length: 7\r\nHost: www.w3.org\r\n\r\na=1&b=2")

	if val = Header(payload, []byte("Content-Length")); !bytes.Equal(val, []byte("7")) {
		t.Error("Should find header value")
	}

	// Value with space at end
	payload = []byte("POST /post HTTP/1.1\r\nContent-Length: 7 \r\nHost: www.w3.org\r\n\r\na=1&b=2")

	if val = Header(payload, []byte("Content-Length")); !bytes.Equal(val, []byte("7")) {
		t.Error("Should find header value without space after 7")
	}

	// Value without space at start
	payload = []byte("POST /post HTTP/1.1\r\nContent-Length:7\r\nHost: www.w3.org\r\n\r\na=1&b=2")

	if val = Header(payload, []byte("Content-Length")); !bytes.Equal(val, []byte("7")) {
		t.Error("Should find header value without space after :")
	}

	// Value is empty
	payload = []byte("GET /p HTTP/1.1\r\nCookie:\r\nHost: www.w3.org\r\n\r\n")

	if val = Header(payload, []byte("Cookie")); len(val) > 0 {
		t.Error("Should return empty value")
	}

	// Header not found
	if _, headerStart, _, _, _ = header(payload, []byte("Not-Found")); headerStart != -1 {
		t.Error("Should not found header")
	}

	// Lower case headers
	payload = []byte("POST /post HTTP/1.1\r\ncontent-length: 7\r\nHost: www.w3.org\r\n\r\na=1&b=2")

	if val = Header(payload, []byte("Content-Length")); !bytes.Equal(val, []byte("7")) {
		t.Error("Should find lower case 2 word header")
	}

	payload = []byte("POST /post HTTP/1.1\r\ncontent-length: 7\r\nhost: www.w3.org\r\n\r\na=1&b=2")

	if val = Header(payload, []byte("host")); !bytes.Equal(val, []byte("www.w3.org")) {
		t.Error("Should find lower case 1 word header")
	}
}

func TestMIMEHeadersEndPos(t *testing.T) {
	head := []byte("POST /post HTTP/1.1\r\nContent-Length: 7\r\nHost: www.w3.org\r\n\r\n")
	payload := []byte("POST /post HTTP/1.1\r\nContent-Length: 7\r\nHost: www.w3.org\r\n\r\na=1&b=2")

	end := MIMEHeadersEndPos(payload)

	if !bytes.Equal(payload[:end], head) {
		t.Error("Wrong headers end position:", end, head, payload[:end])
	}
}

func TestMIMEHeadersStartPos(t *testing.T) {
	headers := []byte("Content-Length: 7\r\nHost: www.w3.org")
	payload := []byte("POST /post HTTP/1.1\r\nContent-Length: 7\r\nHost: www.w3.org\r\n\r\na=1&b=2")

	start := MIMEHeadersStartPos(payload)
	end := MIMEHeadersEndPos(payload) - 4

	if !bytes.Equal(payload[start:end], headers) {
		t.Error("Wrong headers end position:", start, end, payload[start:end])
	}
}

func TestHasResponseTitle(t *testing.T) {
	var m = map[string]bool{
		"HTTP":                      false,
		"":                          false,
		"HTTP/1.1 100 Continue":     false,
		"HTTP/1.1 100 Continue\r\n": true,
		"HTTP/1.1  \r\n":            false,
		"HTTP/4.0 100Continue\r\n":  false,
		"HTTP/1.0 100Continue\r\n":  false,
		"HTTP/1.0 10r Continue\r\n": false,
		"HTTP/1.1 200\r\n":          true,
		"HTTP/1.1 200\r\nServer: Tengine\r\nContent-Length: 0\r\nConnection: close\r\n\r\n": true,
	}
	for k, v := range m {
		if HasResponseTitle([]byte(k)) != v {
			t.Errorf("%q should yield %v", k, v)
			break
		}
	}
}

func TestHasRequestTitle(t *testing.T) {
	var m = map[string]bool{
		"POST /post HTTP/1.0\r\n": true,
		"":                        false,
		"POST /post HTTP/1.\r\n":  false,
		"POS /post HTTP/1.1\r\n":  false,
		"GET / HTTP/1.1\r\n":      true,
		"GET / HTTP/1.1\r":        false,
		"GET / HTTP/1.400\r\n":    false,
	}
	for k, v := range m {
		if HasRequestTitle([]byte(k)) != v {
			t.Errorf("%q should yield %v", k, v)
			break
		}
	}
}

func TestHasFullPayload(t *testing.T) {
	var m string
	var got, expected bool

	got = HasFullPayload(nil,
		[]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n"+
			"Transfer-Encoding: chunked\r\n\r\n"+
			"7\r\nMozilla\r\n9\r\nDeveloper\r\n"+
			"7\r\nNetwork\r\n0\r\n\r\n"))
	expected = true
	if got != expected {
		t.Errorf("expected %v to equal %v", got, expected)
	}

	// check chunks with trailers
	m = "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nTransfer-Encoding: chunked\r\nTrailer: Expires\r\n\r\n7\r\nMozilla\r\n9\r\nDeveloper\r\n7\r\nNetwork\r\n0\r\n\r\nExpires: Wed, 21 Oct 2015 07:28:00 GMT\r\n\r\n"
	got = HasFullPayload(nil, []byte(m))
	expected = true
	if got != expected {
		t.Errorf("expected %v to equal %v", got, expected)
	}

	// check with missing trailers
	m = "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nTransfer-Encoding: chunked\r\nTrailer: Expires\r\n\r\n7\r\nMozilla\r\n9\r\nDeveloper\r\n7\r\nNetwork\r\n0\r\n\r\nExpires: Wed, 21 Oct 2015 07:28:00"
	got = HasFullPayload(nil, []byte(m))
	expected = false
	if got != expected {
		t.Errorf("expected %v to equal %v", got, expected)
	}

	// check with content-length
	m = "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 23\r\n\r\nMozillaDeveloperNetwork"
	got = HasFullPayload(nil, []byte(m))
	expected = true
	if got != expected {
		t.Errorf("expected %v to equal %v", got, expected)
	}

	// check missing total length
	m = "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 23\r\n\r\nMozillaDeveloperNet"
	got = HasFullPayload(nil, []byte(m))
	expected = false
	if got != expected {
		t.Errorf("expected %v to equal %v", got, expected)
	}

	// check with no body
	m = "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n"
	got = HasFullPayload(nil, []byte(m))
	expected = true
	if got != expected {
		t.Errorf("expected %v to equal %v", got, expected)
	}
}

func BenchmarkHasFullPayload(b *testing.B) {
	data := []byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nTransfer-Encoding: chunked\r\n\r\n1e\r\n111111111111111111111111111111\r\n0\r\n\r\n")
	for i := 0; i < b.N; i++ {
		if !HasFullPayload(nil, data) {
			b.Fail()
		}
	}
}
