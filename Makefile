BINARY := haproxy-bridge
VERSION ?= dev
.PHONY: build test integration release
build:
	go build -trimpath -ldflags '-X main.version=$(VERSION)' -o bin/$(BINARY) ./cmd/haproxy-bridge
test:
	go test -race ./...
	go vet ./...
integration:
	HAPROXY_BRIDGE_INTEGRATION=1 go test -count=1 -v ./integration
release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/$(BINARY)-linux-amd64 ./cmd/haproxy-bridge
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/$(BINARY)-linux-arm64 ./cmd/haproxy-bridge
	cd dist && shasum -a 256 $(BINARY)-linux-* > SHA256SUMS
