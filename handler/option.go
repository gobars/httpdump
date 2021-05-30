package handler

import (
	"sync/atomic"

	"github.com/bingoohuang/httpdump/util"
)

const (
	LevelUrl    = "url"
	LevelHeader = "header"
)

type Option struct {
	Resp     bool
	Host     string
	Uri      string
	Method   string
	Status   util.IntSetFlag
	Level    string
	DumpBody string
	dumpNum  uint32
	DumpMax  uint32
	Force    bool
	Curl     bool
}

func (o *Option) CanDump() bool {
	if o.DumpBody == "" {
		return false
	}

	return o.DumpMax <= 0 || atomic.LoadUint32(&o.dumpNum) < o.DumpMax
}
