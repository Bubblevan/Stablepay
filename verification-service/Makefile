.PHONY: build test test-race fmt vet run

build:
	go build ./cmd/server

test:
	go test ./...

test-race:
	go test -race -coverprofile=coverage.out ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

run:
	go run ./cmd/server
