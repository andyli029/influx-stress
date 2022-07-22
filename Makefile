### Makefile ---

PROGRAM     := influx-stress
LDFLAGS     ?= "-s -w"
GOBUILD_ENV = GO111MODULE=on CGO_ENABLED=0
GOBUILD     = go build -o bin/$(PROGRAM) -a -ldflags $(LDFLAGS) github.com/chengshiwen/influx-stress/cmd/influx-stress
GOX         = go run github.com/mitchellh/gox
TARGETS     := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64
DIST_DIRS   := find * -maxdepth 0 -type d -exec

.PHONY: build linux cross-build release test help insert lint clean

all: build

build:
	$(GOBUILD_ENV) $(GOBUILD)

linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD_ENV) $(GOBUILD)

cross-build: clean
	$(GOBUILD_ENV) $(GOX) -ldflags $(LDFLAGS) -parallel=5 -output="bin/$(PROGRAM)-{{.OS}}-{{.Arch}}/$(PROGRAM)" -osarch='$(TARGETS)' ./cmd/...

release: cross-build
	( \
		cd bin && \
		$(DIST_DIRS) cp ../LICENSE {} \; && \
		$(DIST_DIRS) cp ../README.md {} \; && \
		$(DIST_DIRS) tar -zcf {}.tar.gz {} \; && \
		$(DIST_DIRS) zip -r {}.zip {} \; && \
		$(DIST_DIRS) rm -rf {} \; && \
		sha256sum * > sha256sums.txt \
	)

test:
	go test -v github.com/chengshiwen/influx-stress/lineprotocol
	go test -v github.com/chengshiwen/influx-stress/point
	go test -v github.com/chengshiwen/influx-stress/write

help:
	go run cmd/influx-stress/main.go help

insert:
	go run cmd/influx-stress/main.go insert -r 10s -f

lint:
	golangci-lint run --enable=golint --disable=errcheck --disable=typecheck
	goimports -l -w .
	go fmt ./...
	go vet ./...

clean:
	rm -rf bin

### Makefile ends here
