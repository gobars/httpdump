package util

import (
	"github.com/bingoohuang/gg/pkg/ss"
	"log"
	"net/http"
	"net/http/httputil"
)

func LogResponse(r *http.Response, verbose string) {
	if r != nil && ss.ContainsAny(verbose, "rsp", "all") {
		if dump, err := httputil.DumpResponse(r, true); err != nil {
			log.Printf("failed to dump response: %v", err)
		} else {
			log.Printf("dumped response: %s", dump)
		}
	}
}

func LogRequest(r *http.Request, verbose string) {
	if r != nil && ss.ContainsAny(verbose, "req", "all") {
		if dump, err := httputil.DumpRequest(r, true); err != nil {
			log.Printf("failed to dump request: %v", err)
		} else {
			log.Printf("dumped request: %s", dump)
		}
	}
}
