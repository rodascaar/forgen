.PHONY: build test test-race lint vet fmt dev install clean bench release

BINARY := forgen
MODULE := github.com/rodascaar/forgen
VERSION ?= 0.1.3
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/app.Version=$(VERSION) -X $(MODULE)/internal/app.Commit=$(COMMIT)

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) ./cmd/forgen

install:
	go install -trimpath -ldflags="$(LDFLAGS)" ./cmd/forgen

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

test-cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

bench:
	go test ./... -run=^$$ -bench=. -benchmem

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:
	golangci-lint run ./...

dev:
	go run ./cmd/forgen

# build multi-plataforma para release (requiere compiladores cruzados)
release:
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_darwin_arm64 ./cmd/forgen
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_darwin_amd64 ./cmd/forgen
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_linux_arm64 ./cmd/forgen
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_linux_amd64 ./cmd/forgen
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_windows_amd64.exe ./cmd/forgen
	cd dist && shasum -a 256 * > checksums.txt

clean:
	rm -rf bin dist coverage.out coverage.html