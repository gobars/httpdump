.PHONY: test install git.commit git.branch default vendor
all: test install

app=$(notdir $(shell pwd))
appVersion := 1.0.0
goVersion := $(shell go version | sed 's/go version //'|sed 's/ /_/')
# e.g. 2021-10-28T11:49:52+0800
buildTime := $(shell date +%FT%T%z)
# https://git-scm.com/docs/git-rev-list#Documentation/git-rev-list.txt-emaIem
# e.g. ffd23d3@2022-04-06T18:07:14+08:00
gitCommit := $(shell [ -f git.commit ] && cat git.commit || git log --format=format:'%h@%aI' -1)
gitBranch := $(shell [ -f git.branch ] && cat git.branch || git rev-parse --abbrev-ref HEAD)
gitInfo = $(gitBranch)-$(gitCommit)
#gitCommit := $(shell git rev-list -1 HEAD)
# https://stackoverflow.com/a/47510909
pkg := github.com/bingoohuang/gg/pkg/v

extldflags := -linkmode external -extldflags -static
# https://ms2008.github.io/2018/10/08/golang-build-version/
# https://github.com/kubermatic/kubeone/blob/master/Makefile
flags1 = -s -w -X $(pkg).BuildTime=$(buildTime) -X $(pkg).AppVersion=$(appVersion) -X $(pkg).GitCommit=$(gitInfo) -X $(pkg).GoVersion=$(goVersion)
flags2 = ${extldflags} ${flags1}
goinstall1 = go install -mod vendor -trimpath -ldflags='${flags1}' ./...
goinstall = go install -mod vendor -trimpath -ldflags='${flags2}' ./...
gobin := $(shell go env GOBIN)
# try $GOPATN/bin if $gobin is empty
gobin := $(if $(gobin),$(gobin),$(shell go env GOPATH)/bin)

export GOPROXY=https://mirrors.aliyun.com/goproxy/,https://goproxy.cn,https://goproxy.io,direct
# Active module mode, as we use go modules to manage dependencies
export GO111MODULE=on

git.commit:
	echo ${gitCommit} > git.commit
	echo ${gitBranch} > git.branch

tool:
	go get github.com/securego/gosec/cmd/gosec

sec:
	@gosec ./...
	@echo "[OK] Go security check was completed!"

init:

lint-all:
	golangci-lint run --enable-all

lint:
	golangci-lint run ./...

fmt:
	gofumpt -l -w .
	gofmt -s -w .
	go mod tidy
	go fmt ./...
	revive .
	goimports -w .
	gci -w -local github.com/daixiang0/gci

install1: init
	${goinstall1}
	upx --best --lzma ${gobin}/${app}
	ls -lh ${gobin}/${app}

install: init
	${goinstall}
	upx --best --lzma ${gobin}/${app}
	ls -lh ${gobin}/${app}
linux: init
	GOOS=linux GOARCH=amd64 ${goinstall}
	upx --best --lzma ${gobin}/linux_amd64/${app}
	ls -lh  ${gobin}/linux_amd64/${app}
arm: init
	GOOS=linux GOARCH=arm64 ${goinstall}
	upx --best --lzma ${gobin}/linux_arm64/${app}
	ls -lh  ${gobin}/linux_arm64/${app}

upx:
	ls -lh ${gobin}/${app}
	upx ${gobin}/${app}
	ls -lh ${gobin}/${app}
	ls -lh ${gobin}/linux_amd64/${app}
	upx ${gobin}/linux_amd64/${app}
	ls -lh ${gobin}/linux_amd64/${app}

test: init
	#go test -v ./...
	go test -v -race ./...

bench: init
	#go test -bench . ./...
	go test -tags bench -benchmem -bench . ./...

clean:
	rm coverage.out

cover:
	go test -v -race -coverpkg=./... -coverprofile=coverage.out ./...

coverview:
	go tool cover -html=coverage.out

# https://hub.docker.com/_/golang
# docker run --rm -v "$PWD":/usr/src/myapp -v "$HOME/dockergo":/go -w /usr/src/myapp golang make docker
# docker run --rm -it -v "$PWD":/usr/src/myapp -w /usr/src/myapp golang bash
# 静态连接 glibc
docker:
	mkdir -p ~/dockergo
	docker run --rm -v "$$PWD":/usr/src/myapp -v "$$HOME/dockergo":/go -w /usr/src/myapp golang make dockerinstall
	#upx ~/dockergo/bin/${app}
	gzip -f ~/dockergo/bin/${app}

dockerinstall:
	go install -v -x -a -ldflags=${flags} ./...

targz:
	find . -name ".DS_Store" -delete
	find . -type f -name '\.*' -print
	cd .. && rm -f ${app}.tar.gz && tar czvf ${app}.tar.gz --exclude .git --exclude .idea ${app}


vendor:
	go mod download && go mod vendor

# linux amd64 跨平台编译，libpcap包静态链接
amd64-docker:
	docker build -f build/Dockerfile  -t golang-new-builder-amd:v1.22.3 .
amd64: git.commit
	docker run -it --rm -v .:/code -w /code golang-new-builder-amd:v1.22.3 --build-cmd "make build-docker" -p "linux/amd64"
build-docker:
	${goinstall1}
	cp `which httpdump` .
	ldd --version
	-ldd ./httpdump
	ls -hl ./httpdump
	./httpdump -v
	./httpdump -h


