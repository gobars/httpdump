package replay

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPClient holds configurations for a single HTTP client
type HTTPClient struct {
	Client *http.Client
	*HTTPClientConfig
}

type HTTPClientConfig struct {
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
	start := time.Now()
	resp, err := c.Client.Do(req)
	result := &SendResponse{
		Method: req.Method,
		URL:    req.URL.String(),
		Cost:   time.Since(start),
	}
	if resp != nil {
		var b bytes.Buffer
		io.Copy(&b, resp.Body)
		resp.Body.Close()
		result.ResponseBody = b.Bytes()
		result.StatusCode = resp.StatusCode
	}

	return result, err
}
