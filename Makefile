.PHONY: build install clean

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X github.com/lazaroagomez/wusbkit/cmd.Version=$(VERSION) -X github.com/lazaroagomez/wusbkit/cmd.BuildDate=$(BUILD_DATE)"

build:
	@mkdir -p dist
	go build $(LDFLAGS) -o dist/wusbkit.exe .

install:
	go install $(LDFLAGS) .

clean:
	rm -rf dist/
