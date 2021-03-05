# Makefile for cke

ETCD_VERSION = 3.3.25

.PHONY: all
all: test

.PHONY: setup
setup:
	curl -fsL https://github.com/etcd-io/etcd/releases/download/v$(ETCD_VERSION)/etcd-v$(ETCD_VERSION)-linux-amd64.tar.gz | sudo tar -xzf - --strip-components=1 -C /usr/local/bin etcd-v$(ETCD_VERSION)-linux-amd64/etcd etcd-v$(ETCD_VERSION)-linux-amd64/etcdctl

.PHONY: test
test: test-tools
	test -z "$$(gofmt -s -l . | tee /dev/stderr)"
	staticcheck ./...
	test -z "$$(nilerr ./... 2>&1 | tee /dev/stderr)"
	test -z "$$(custom-checker -restrictpkg.packages=html/template,log ./... 2>&1 | tee /dev/stderr)"
	go install ./pkg/...
	go test -race -v ./...
	go vet ./...

.PHONY: static
static: goimports
	go generate ./static
	git add ./static/resources.go

.PHONY: test-tools
test-tools: staticcheck nilerr goimports custom-checker

.PHONY: staticcheck
staticcheck:
	if ! which staticcheck >/dev/null; then \
		env GOFLAGS= go install honnef.co/go/tools/cmd/staticcheck@latest; \
	fi

.PHONY: nilerr
nilerr:
	if ! which nilerr >/dev/null; then \
		env GOFLAGS= go install github.com/gostaticanalysis/nilerr/cmd/nilerr@latest; \
	fi

.PHONY: goimports
goimports:
	if ! which goimports >/dev/null; then \
		cd /tmp; env GOFLAGS= GO111MODULE=on go get golang.org/x/tools/cmd/goimports; \
	fi

.PHONY: custom-checker
custom-checker:
	if ! which custom-checker >/dev/null; then \
		cd /tmp; env GOFLAGS= GO111MODULE=on go get github.com/cybozu/neco-containers/golang/analyzer/cmd/custom-checker; \
	fi
