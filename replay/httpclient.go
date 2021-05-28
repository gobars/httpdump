package replay

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/bingoohuang/gg/pkg/ss"
)

// HTTPClient holds configurations for a single HTTP client
type HTTPClient struct {
	Client *http.Client
	*HTTPClientConfig
}

type HTTPClientConfig struct {
	Verbose        string // (empty)/req/rsp/all
	Timeout        time.Duration
	InsecureVerify bool
	BaseURL        *url.URL
	Methods        string
}

// NewHTTPClient returns new http client with check redirects policy
func NewHTTPClient(c *HTTPClientConfig) *HTTPClient {
	if c.Timeout == 0 {
		c.Timeout = 15 * time.Second
	}
	client := &HTTPClient{
		HTTPClientConfig: c,
		Client: &http.Client{
			Timeout: c.Timeout,
		},
	}
	if !c.InsecureVerify {
		// clone to avoid modying global default RoundTripper
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		client.Client.Transport = t
	}

	return client
}

type SendResponse struct {
	Method       string
	URL          string
	ResponseBody []byte
	StatusCode   int
	Cost         time.Duration
}

// Send sends an http request using client create by NewHTTPClient
func (c *HTTPClient) Send(data []byte) (*SendResponse, error) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return nil, err
	}
	// we don't send CONNECT or OPTIONS request
	if req.Method == http.MethodConnect {
		return nil, nil
	}
	if c.Methods != "" && !strings.Contains(c.Methods, req.Method) {
		return nil, nil
	}

	// avoid replay
	if req.Header.Get("X-Goreplay-Output") == "1" {
		return nil, nil
	}
	req.Header.Set("X-Goreplay-Output", "1")
	req.Host = c.BaseURL.Host
	req.URL.Host = c.BaseURL.Host
	req.URL.Scheme = c.BaseURL.Scheme

	// force connection to not be closed, which can affect the global client
	req.Close = false
	// it's an error if this is not equal to empty string
	req.RequestURI = ""

	logRequestDump(req, c.Verbose)

	start := time.Now()
	rsp, err := c.Client.Do(req)
	sendRsp := &SendResponse{
		Method: req.Method,
		URL:    req.URL.String(),
		Cost:   time.Since(start),
	}

	logResponseDump(rsp, c.Verbose)

	if rsp != nil {
		sendRsp.ResponseBody, _ = ReadCloseBody(rsp)
		sendRsp.StatusCode = rsp.StatusCode
	}

	return sendRsp, err
}

func ReadCloseBody(r *http.Response) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func logResponseDump(r *http.Response, verbose string) {
	if r != nil && ss.ContainsAny(verbose, "rsp", "all") {
		if dump, err := httputil.DumpResponse(r, true); err != nil {
			log.Printf("failed to dump response: %v", err)
		} else {
			log.Printf("dumped response: %s", dump)
		}
	}
}

func logRequestDump(r *http.Request, verbose string) {
	if r != nil && ss.ContainsAny(verbose, "req", "all") {
		if dump, err := httputil.DumpRequest(r, true); err != nil {
			log.Printf("failed to dump request: %v", err)
		} else {
			log.Printf("dumped request: %s", dump)
		}
	}
}
