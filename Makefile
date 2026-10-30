.PHONY: build test vet run

VERSION ?= dev

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/core ./cmd/core

test:
	go test ./...

vet:
	go vet ./...

run:
	go run ./cmd/core
