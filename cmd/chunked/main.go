package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	for i := 0; i < 10000; i++ {
		time.Sleep(5 * time.Millisecond)
		demoChunkedTransferEncodingInHttpClient()
	}
}

func demoChunkedTransferEncodingInHttpClient() {
	r := &http.Request{
		Method: "POST",
		URL: &url.URL{
			Scheme: "http",
			Host:   "localhost:5003",
			Path:   "/solr/licenseIndex/update?wt=javabin&version=2",
		},
		Header: http.Header{
			"Content-Type": {"application/xml; charset=UTF-8"},
		},
		ContentLength: -1,
		Body:          ioutil.NopCloser(strings.NewReader(`<text>Hello world!</text>`)),
	}
	//r.Write(os.Stdout), comment out this line before Do http client requesting.

	rr, err := http.DefaultClient.Do(r)
	if err != nil {
		fmt.Println(err)
	} else {
		rr.Write(os.Stdout)
		io.Copy(io.Discard, rr.Body)
	}

	//r.Write(os.Stdout)
	// will output:
	/*
		PUT /solr/demo HTTP/1.1
		Host: localhost:5003
		User-Agent: Go-http-client/1.1
		Transfer-Encoding: chunked
		Content-Type: application/json

		d
		Hello, world!
		0
	*/
}
