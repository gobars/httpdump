package main

import (
	"time"
)

// Option Command line options.
type Option struct {
	Level     string        `val:"header" usage:"Output level, options are: url(only url) | header(http headers) | all(headers, and textuary http body)"`
	File      string        `usage:"Read from pcap file. If not set, will capture data from network device by default"`
	Device    string        `val:"any" usage:"Capture packet from network device. If is any, capture all interface traffics"`
	Ip        string        `usage:"Filter by ip, if either source or target ip is matched, the packet will be processed"`
	Port      uint          `usage:"Filter by port, if either source or target port is matched, the packet will be processed."`
	Chan      uint          `val:"10240" usage:"Channel size to buffer tcp packets."`
	Host      string        `usage:"Filter by request host, using wildcard match(*, ?)"`
	Uri       string        `usage:"Filter by request url path, using wildcard match(*, ?)"`
	PrintResp bool          `usage:"Print response or not"`
	Status    Status        `usage:"Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400"`
	Force     bool          `usage:"Force print unknown content-type http body even if it seems not to be text content"`
	Curl      bool          `usage:"Output an equivalent curl command for each http request"`
	DumpBody  bool          `usage:"dump http request/response body to file"`
	Output    string        `usage:"Write result to file [output] instead of stdout"`
	Idle      time.Duration `val:"4m" usage:"Idle time to remove connection if no package received"`
}

type Status IntSet

func (i *Status) String() string { return "" }

func (i *Status) Set(value string) error {
	set, err := ParseIntSet(value)
	if err != nil {
		return err
	}
	*i = Status(*set)
	return nil
}
