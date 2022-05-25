package handler

import (
	"strings"
	"sync/atomic"

	"golang.org/x/time/rate"

	"github.com/bingoohuang/httpdump/util"
)

const (
	LevelUrl    = "url"
	LevelHeader = "header"
)

type Option struct {
	Resp        bool
	Host        string
	Uri         string
	Method      string
	Status      util.IntSetFlag
	Level       string
	DumpBody    string
	dumpNum     uint32
	DumpMax     uint32
	Force       bool
	Curl        bool
	RateLimiter *rate.Limiter
}

func (o *Option) CanDump() bool {
	if o.DumpBody == "" {
		return false
	}

	return o.DumpMax <= 0 || atomic.LoadUint32(&o.dumpNum) < o.DumpMax
}

func (o *Option) PermitsMethod(method string) bool {
	return o.Method == "" || strings.Contains(o.Method, method)
}

func (o *Option) PermitsUri(uri string) bool { return o.Uri == "" || wildcardMatch(uri, o.Uri) }

func (o *Option) PermitsHost(host string) bool { return o.Host == "" || wildcardMatch(host, o.Host) }

func (o *Option) Permits(r Req) bool {
	return o.PermitsHost(r.GetHost()) && o.PermitsUri(r.GetRequestURI()) && o.PermitsMethod(r.GetMethod())
}

func (o *Option) PermitsCode(code int) bool { return o.Status.Contains(code) }
