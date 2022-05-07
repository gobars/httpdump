# httpdump

Parse and display http traffic from network device or pcap file. This is a go version of origin pcap-parser, thanks to
gopacket project, this tool has simpler code base and is more efficient.

Forked from [httpdump](https://github.com/hsiafan/httpdump), For original python
implementation, [refer to httpcap on pypi](https://pypi.org/project/httpcap/).

## Install & Requirement

Build httpdump requires libpcap-dev and cgo enabled.

### libpcap

1. for ubuntu/debian: `sudo apt install libpcap-dev`
1. for centos/redhat/fedora: `sudo yum install libpcap-devel`
1. for osx: Libpcap and header files should be available in macOS already.

### Install

`make install`

## Cheatsheet

1. 监听发往 192.168.1.1:80 的 HTTP POST 请求及响应，并且写到日志文件 `log-yyyy-MM-dd.http` 中，按 100m 滚动(例如 log-yyyy-MM-dd_00001.http)，同时往
   192.168.1.2:80 复制。

`nohup httpdump -bpf "tcp and ((dst host 192.168.1.1 and port 80) || (src host 192.168.1.1 and src port 80))" -method POST -output log-yyyy-MM-dd.http:100m -output 192.168.1.2:80 2>&1 >> httpdump.nohup &`

## Usage

httpdump can read from pcap file, or capture data from network interfaces. Usage:

```sh
$ httpdump -h
Usage of httpdump:
  -bpf string	Customized bpf, if it is set, -ip -port will be suppressed
  -c string	yaml config filepath
  -chan uint	Channel size to buffer tcp packets (default 10240)
  -curl	Output an equivalent curl command for each http request
  -dump-body string	Prefix file of dump http request/response body, empty for no dump, like solr, solr:10 (max 10)
  -f string	File of http request to parse, glob pattern like data/*.gor, or path like data/, suffix :tail to tail files, suffix :poll to set the tail watch method to poll
  -fla9 string	Flags config file, a scaffold one will created when it does not exist.
  -force	Force print unknown content-type http body even if it seems not to be text content
  -host string	Filter by request host, using wildcard match(*, ?)
  -i string	Interface name or pcap file. If not set, If is any, capture all interface traffics (default "any")
  -idle duration	Idle time to remove connection if no package received (default 4m0s)
  -init	init example httpdump.yml/ctl and then exit
  -ip string	Filter by ip, if either src or dst ip is matched, the packet will be processed
  -level string	Output level, url: only url, header: http headers, all: headers and text http body (default "all")
  -method string	Filter by request method, multiple by comma
  -mode string	std/fast (default "fast")
  -out-chan uint	Output channel size to buffer tcp packets (default 40960)
  -output value	File output, like dump-yyyy-MM-dd-HH-mm.http, suffix like :32m for max size, suffix :append for append mode
 Or Relay http address, eg http://127.0.0.1:5002
  -port uint	Filter by port, if either source or target port is matched, the packet will be processed
  -pprof string	pprof address to listen on, not activate pprof if empty, eg. :6060
  -resp	Print response or not
  -status value	Filter by response status code. Can use range. eg: 200, 200-300 or 200:300-400
  -uri string	Filter by request url path, using wildcard match(*, ?)
  -v	Print version info and exit
  -verbose string	Verbose flag, available req/rsp/all for http replay dump
  -web	Start web server for HTTP requests and responses event
  -web-port int	Web server port if web is enable
```

## Samples

A simple capture:

```sh
🕙[2021-05-22 18:05:03.891] ❯ sudo httpdump -i lo0 -port 5003 -resp -level all

### REQUEST  ::1:59982 ea4e138b00000001b295aafb -> ::1:5003 2021-05-22T18:05:16.065566+08:00
POST /echo/123 HTTP/1.1
Content-Length: 18
Host: localhost:5003
User-Agent: HTTPie/2.4.0
Accept-Encoding: gzip, deflate
Accept: application/json, */*;q=0.5
Connection: keep-alive
Content-Type: application/json

{
    "name": "bingoo"
}


### RESPONSE  ::1:59982 ea4e138b00000001b295aafb <- ::1:5003 2021-05-22T18:05:16.065566+08:00 - 2021-05-22T18:05:16.065566+08:00 = 0s
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
Date: Sat, 22 May 2021 10:05:16 GMT
Content-Length: 474

{
    "headers": {
        "Accept": "application/json, */*;q=0.5",
        "Accept-Encoding": "gzip, deflate",
        "Connection": "keep-alive",
        "Content-Length": "18",
        "Content-Type": "application/json",
        "User-Agent": "HTTPie/2.4.0"
    },
    "host": "localhost:5003",
    "method": "POST",
    "payload": {
        "name": "bingoo"
    },
    "proto": "HTTP/1.1",
    "remoteAddr": "[::1]:59982",
    "requestUri": "/echo/123",
    "router": "/echo/:id",
    "routerParams": {
        "id": "123"
    },
    "timeGo": "2021-05-22 18:05:16.0625",
    "timeTo": "2021-05-22 18:05:16.0625",
    "url": "/echo/123"
}
```

More:

```sh
# parse pcap file
sudo tcpdump -wa.pcap tcp
httpdump -i a.pcap

# capture specified device:
httpdump -i eth0

# filter by ip and/or port
httpdump -port 80  # filter by port
httpdump -ip 101.201.170.152 # filter by ip
httpdump -ip 101.201.170.152 -port 80 # filter by ip and port
```

## Help

抓取到指定IP端口的请求及相应的bpf

`httpdump -bpf "tcp and ((dst host 192.168.1.1 and dst port 5003) or (src host 192.168.1.1 and src port 5003))"  -method POST`

## 部署

1. 查看版本：`./httpdump -v` 最新版本是：httpdump v1.2.7 2021-06-21 14:13:46
1. 生成启停命令文件 和 样例 yml 配置文件  `./httpdump -init`
2. 编辑 yml 配置文件 `httpdump.yml`，调整取值
3. ./ctl help 查看帮助， `./ctl start` 启动
4. 限制CPU在2个核上共占20% 启动 `LIMIT_CPU=20 LIMIT_CORES=2 ./ctl start`，（需要linux安装了cgroups包)

httpdump.yml 配置示例:

```yml
# 监听 ip
ip: 192.168.126.5
# 监听 端口
port: 5003
# 注意：ip 和 port 同时配置时，相当于设置了 bpf: tcp and ((dst host {ip} and dst port {port}) or (src host {ip} and src port {port}))

# 监听 http 方法
method: POST
# 输出 http 请求包
output:
  - post-yyyy-MM-dd.log:100M     # 记录到日志文件，按天滚动，每个文件最大100M
  - "http://192.168.126.18:5003" # 重放到其它服务
  # - stdout
```

## 删除大量文件

`find . -type f -name 'log-*'  -delete`

## 采集 CPU profile

1. 在工作目录下：`touch jj.cpu; kill -USR1 {pid}`，开始采集，等待 5-10 分钟，再次执行相同命令，结束采集，可以在当前目录下看到生成的 cpu.profile`文件
2. 下载文件到本地，使用go工具链查看，例如： `go tool pprof -http :9402 cpu.profile`

## Web UI

`sudo httpdump -port 5003 -resp -web -web-port 6003 -web-context httpdump`

![img.png](_doc/img.png)

## PRINT_JSON=Y

```sh
$ sudo PRINT_JSON=Y httpdump -i lo0 -port 5003 -resp -level all
{"seq":1,"src":"127.0.0.1:58091","dest":"127.0.0.1:5003","timestamp":"2022-05-07T19:01:02.995866+08:00","requestUri":"/backup/person/doc/28plAIG37D36wdbG2J1jcKZumjO","method":"POST","host":"127.0.0.1:5003","header":{"Accept-Encoding":["gzip"],"Content-Type":["application/json"],"Host":["127.0.0.1:5003"],"User-Agent":["Go-http-client/1.1"]},"body":"{\"addr\":\"辽宁省抚顺市日舀路3371号呧媏小区13单元1752室\",\"idcard\":\"516901201412029865\",\"name\":\"庄噛鼶\",\"sex\":\"女\"}\n"}
{"seq":1,"src":"127.0.0.1:58091","dest":"127.0.0.1:5003","timestamp":"2022-05-07T19:01:02.995916+08:00","header":{"Content-Encoding":["gzip"],"Content-Length":["571"],"Content-Type":["application/json; charset=utf-8"],"Date":["Sat, 07 May 2022 11:01:02 GMT"],"Vary":["Accept-Encoding"]},"body":"{\n    \"Ua-Bot\": false,\n    \"Ua-Browser\": \"Go-http-client\",\n    \"Ua-BrowserVersion\": \"1.1\",\n    \"Ua-Engine\": \"\",\n    \"Ua-EngineVersion\": \"\",\n    \"Ua-Localization\": \"\",\n    \"Ua-Mobile\": false,\n    \"Ua-Mozilla\": \"\",\n    \"Ua-OS\": \"\",\n    \"Ua-OSInfo\": {\n        \"FullName\": \"\",\n        \"Name\": \"\",\n        \"Version\": \"\"\n    },\n    \"Ua-Platform\": \"\",\n    \"headers\": {\n        \"Accept-Encoding\": \"gzip\",\n        \"Content-Type\": \"application/json\",\n        \"User-Agent\": \"Go-http-client/1.1\"\n    },\n    \"host\": \"127.0.0.1:5003\",\n    \"method\": \"POST\",\n    \"payload\": {\n        \"addr\": \"辽宁省抚顺市日舀路3371号呧媏小区13单元1752室\",\n        \"idcard\": \"516901201412029865\",\n        \"name\": \"庄噛鼶\",\n        \"sex\": \"女\"\n    },\n    \"proto\": \"HTTP/1.1\",\n    \"remoteAddr\": \"127.0.0.1:58091\",\n    \"requestUri\": \"/backup/person/doc/28plAIG37D36wdbG2J1jcKZumjO\",\n    \"router\": \"/backup/*other\",\n    \"routerParams\": {\n        \"other\": \"/person/doc/28plAIG37D36wdbG2J1jcKZumjO\"\n    },\n    \"timeGo\": \"2022-05-07 19:01:02.9950\",\n    \"timeTo\": \"2022-05-07 19:01:02.9950\",\n    \"transferEncoding\": \"chunked\",\n    \"url\": \"/backup/person/doc/28plAIG37D36wdbG2J1jcKZumjO\"\n}","statusCode":200}
{"seq":1,"src":"127.0.0.1:58097","dest":"127.0.0.1:5003","timestamp":"2022-05-07T19:01:04.3194+08:00","requestUri":"/backup/person/doc/28plAUob6c4JZUZBaKEPtUo7JQc","method":"POST","host":"127.0.0.1:5003","header":{"Accept-Encoding":["gzip"],"Content-Type":["application/json"],"Host":["127.0.0.1:5003"],"User-Agent":["Go-http-client/1.1"]},"body":"{\"addr\":\"吉林省四平市襻螆路1496号斨炗小区18单元1504室\",\"idcard\":\"716848200911090305\",\"name\":\"荀襽碷\",\"sex\":\"男\"}\n"}
{"seq":1,"src":"127.0.0.1:58097","dest":"127.0.0.1:5003","timestamp":"2022-05-07T19:01:04.319436+08:00","header":{"Content-Encoding":["gzip"],"Content-Length":["571"],"Content-Type":["application/json; charset=utf-8"],"Date":["Sat, 07 May 2022 11:01:04 GMT"],"Vary":["Accept-Encoding"]},"body":"{\n    \"Ua-Bot\": false,\n    \"Ua-Browser\": \"Go-http-client\",\n    \"Ua-BrowserVersion\": \"1.1\",\n    \"Ua-Engine\": \"\",\n    \"Ua-EngineVersion\": \"\",\n    \"Ua-Localization\": \"\",\n    \"Ua-Mobile\": false,\n    \"Ua-Mozilla\": \"\",\n    \"Ua-OS\": \"\",\n    \"Ua-OSInfo\": {\n        \"FullName\": \"\",\n        \"Name\": \"\",\n        \"Version\": \"\"\n    },\n    \"Ua-Platform\": \"\",\n    \"headers\": {\n        \"Accept-Encoding\": \"gzip\",\n        \"Content-Type\": \"application/json\",\n        \"User-Agent\": \"Go-http-client/1.1\"\n    },\n    \"host\": \"127.0.0.1:5003\",\n    \"method\": \"POST\",\n    \"payload\": {\n        \"addr\": \"吉林省四平市襻螆路1496号斨炗小区18单元1504室\",\n        \"idcard\": \"716848200911090305\",\n        \"name\": \"荀襽碷\",\n        \"sex\": \"男\"\n    },\n    \"proto\": \"HTTP/1.1\",\n    \"remoteAddr\": \"127.0.0.1:58097\",\n    \"requestUri\": \"/backup/person/doc/28plAUob6c4JZUZBaKEPtUo7JQc\",\n    \"router\": \"/backup/*other\",\n    \"routerParams\": {\n        \"other\": \"/person/doc/28plAUob6c4JZUZBaKEPtUo7JQc\"\n    },\n    \"timeGo\": \"2022-05-07 19:01:04.3182\",\n    \"timeTo\": \"2022-05-07 19:01:04.3182\",\n    \"transferEncoding\": \"chunked\",\n    \"url\": \"/backup/person/doc/28plAUob6c4JZUZBaKEPtUo7JQc\"\n}","statusCode":200}
```