# https://github.com/elastic/golang-crossbuild/releases/tag/v1.20.11
# linux/i386, linux/amd64, windows/amd64
FROM docker.elastic.co/beats-dev/golang-crossbuild:1.20.11-main

RUN apt-get update && apt-get install -y libpcap-dev

ENV GOPROXY=https://goproxy.cn
ENV CGO_LDFLAGS=-L/libpcap/libpcap-1.8.1-amd64

# 编译镜像 docker build -t golang-new-builder-amd:v1.20.11 .


# 开始编译go 程序 
# docker run -it --rm -v .:/code -w /code golang-new-builder-amd:v1.20.11 -e CGO_LDFLAGS+="-L/libpcap/libpcap-1.8.1-amd64" --build-cmd "go build" -p "linux/amd64"
