package replay

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"log"
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
	RedirectLimit  int
	InsecureVerify bool
	BaseURL        *url.URL
	Methods        string
}

// NewHTTPClient returns new http client with check redirects policy
func NewHTTPClient(c *HTTPClientConfig) *HTTPClient {
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if len(via) >= c.RedirectLimit {
			log.Printf("W! [HTTPCLIENT] max redirects[%d] reached!", c.RedirectLimit)
			return http.ErrUseLastResponse
		}
		lastReq := via[len(via)-1]
		resp := req.Response
		log.Printf("W! [HTTPCLIENT] redirects from %q to %q with %q", lastReq.Host, req.Host, resp.Status)
		return nil
	}
	client := &HTTPClient{
		HTTPClientConfig: c,
		Client: &http.Client{
			Timeout:       c.Timeout,
			CheckRedirect: checkRedirect,
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
	Method string
	URL    string
	*http.Response
	Cost time.Duration
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
	return &SendResponse{
		Method:   req.Method,
		URL:      req.URL.String(),
		Response: resp,
		Cost:     time.Since(start),
	}, err
}
