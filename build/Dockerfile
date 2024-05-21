# https://github.com/elastic/golang-crossbuild/releases/tag/v1.22.3
# https://sourceware.org/glibc/wiki/Glibc%20Timeline
# linux/i386, linux/amd64, windows/amd64
# Debian8 uses glibc 2.19. 2014-02-07
# Debian9 uses glibc 2.24. 2016-08-04
# Debian10 uses glibc 2.28. 2018-08-01
# Debian11 uses glibc 2.31. 2020-02-01
FROM docker.elastic.co/beats-dev/golang-crossbuild:1.22.3-main-debian8

RUN apt-get update && apt-get install -y --force-yes libpcap-dev

ENV GOPROXY=https://goproxy.cn
ENV CGO_LDFLAGS=-L/libpcap/libpcap-1.8.1-amd64

# 编译镜像 docker build -t golang-new-builder-amd:v1.22.3 .


# 开始编译go 程序 
# docker run -it --rm -v .:/code -w /code golang-new-builder-amd:v1.22.3 -e CGO_LDFLAGS+="-L/libpcap/libpcap-1.8.1-amd64" --build-cmd "go build" -p "linux/amd64"
