.PHONY: build test fmt lint run

build:
	go build ./cmd/server

test:
	go test -race -coverprofile=coverage.out ./...

fmt:
	go fmt ./...

lint:
	golangci-lint run

run:
	go run ./cmd/server
