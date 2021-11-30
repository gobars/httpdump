.PHONY: init install
all: init install

app=$(notdir $(shell pwd))
goVersion := $(shell go version | sed 's/go version //'|sed 's/ /_/')
buildTime := $(shell if hash gdate 2>/dev/null; then gdate --rfc-3339=seconds | sed 's/ /T/'; else date --rfc-3339=seconds | sed 's/ /T/'; fi)
# https://git-scm.com/docs/git-rev-list#Documentation/git-rev-list.txt-emaIem
gitCommit := $(shell git rev-list --oneline --format=format:'%h@%aI' --max-count=1 `git rev-parse HEAD` | tail -1)
#gitCommit := $(shell git rev-list -1 HEAD)
# https://stackoverflow.com/a/47510909
pkg := github.com/bingoohuang/gg/pkg/v
appVersion := 1.3.4
extldflags := -extldflags -static
# https://ms2008.github.io/2018/10/08/golang-build-version/
# https://github.com/kubermatic/kubeone/blob/master/Makefile
flags1 = "-s -w -X $(pkg).buildTime=$(buildTime) -X $(pkg).appVersion=$(appVersion) -X $(pkg).gitCommit=$(gitCommit) -X $(pkg).goVersion=$(goVersion)"
flags2 = "$(extldflags) -s -w -X $(pkg).buildTime=$(buildTime) -X $(pkg).appVersion=$(appVersion) -X $(pkg).gitCommit=$(gitCommit) -X $(pkg).goVersion=$(goVersion)"
gobin := $(shell go env GOBIN)

tool:
	go get github.com/securego/gosec/cmd/gosec

sec:
	@gosec ./...
	@echo "[OK] Go security check was completed!"

init:
	export GOPROXY=https://goproxy.cn

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

install: init
	go install -trimpath -ldflags=${flags1}  ./...
	upx ${gobin}/${app}

linux: init
	GOOS=linux GOARCH=amd64 go install -trimpath -ldflags=${flags1}  ./...
	upx ${gobin}/linux_amd64/${app}
linux-arm64: init
	GOOS=linux GOARCH=arm64 go install -trimpath -ldflags=${flags1}  ./...
	upx ${gobin}/linux_arm64/${app}

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
