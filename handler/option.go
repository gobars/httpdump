package handler

import (
	"context"
	"math/rand"
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
	Host        string
	Uri         string
	Method      string
	Status      util.IntSetFlag
	Level       string
	DumpBody    string
	dumpNum     uint32
	DumpMax     uint32
	Resp        int
	Force       bool
	Curl        bool
	Eof         bool
	Debug       bool
	RateLimiter *rate.Limiter

	N   int32
	Num int32

	CtxCancel context.CancelFunc

	SrcRatio float64
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

func (o *Option) PermitsReq(r Req) bool {
	return o.permitsHost(r.GetHost()) && o.permitsUri(r.GetRequestURI()) && o.permitN() && o.PermitRatio()
}

func (o *Option) PermitsCode(code int) bool { return o.Status.Contains(code) }

func (o *Option) permitsUri(uri string) bool { return o.Uri == "" || wildcardMatch(uri, o.Uri) }

func (o *Option) permitsHost(host string) bool { return o.Host == "" || wildcardMatch(host, o.Host) }

func (o *Option) ReachedN() bool {
	reached := o.N > 0 && atomic.LoadInt32(&o.Num) <= 0
	if reached {
		o.CtxCancel()
	}

	return reached
}

func (o *Option) permitN() bool {
	return o.N <= 0 || atomic.AddInt32(&o.Num, -1) >= 0
}

func (o *Option) PermitRatio() bool {
	return o.SrcRatio == 1 || rand.Float64() <= o.SrcRatio
}
