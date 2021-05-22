# httpdump

Parse and display http traffic from network device or pcap file. This is a go version of origin pcap-parser, thanks to gopacket project, this tool has simpler code base and is more efficient.

For original python implementation, [refer to httpcap on pypi](https://pypi.org/project/httpcap/).

## Install & Requirement

Build httpdump requires libpcap-dev and cgo enabled.

### libpcap

1. for ubuntu/debian: `sudo apt install libpcap-dev`
1. for centos/redhat/fedora: `sudo yum install libpcap-devel`
1. for osx: Libpcap and header files should be available in macOS already.

### Install

`go install github.com/bingoohuang/httpdump`

## Usage

httpdump can read from pcap file, or capture data from network interfaces. Usage:

```
Usage of httpdump:
  -curl
    	Output an equivalent curl command for each http request
  -device string
    	Capture packet from network device. If is any, capture all interface traffics (default "any")
  -dump-body
    	dump http request/response body to file
  -file string
    	Read from pcap file. If not set, will capture data from network device by default
  -force
    	Force print unknown content-type http body even if it seems not to be text content
  -host string
    	Filter by request host, using wildcard match(*, ?)
  -idle duration
    	Idle time to remove connection if no package received (default 4m0s)
  -ip string
    	Filter by ip, if either source or target ip is matched, the packet will be processed
  -level string
    	Output level, options are: url(only url) | header(http headers) | all(headers, and textuary http body) (default "header")
  -output string
    	Write result to file [output] instead of stdout
  -port uint
    	Filter by port, if either source or target port is matched, the packet will be processed.
  -print-resp
    	Print response or not
  -status string
    	Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400
  -uri string
    	Filter by request url path, using wildcard match(*, ?)
```

## Samples

A simple capture:

```
$ httpdump

```

More:

```sh
# parse pcap file
sudo tcpdump -wa.pcap tcp
httpdump -file a.pcap

# capture specified device:
httpdump -device eth0

# filter by ip and/or port
httpdump -port 80  # filter by port
httpdump -ip 101.201.170.152 # filter by ip
httpdump -ip 101.201.170.152 -port 80 # filter by ip and port
```

