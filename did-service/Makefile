.PHONY: build test test-race fmt run

build:
	go build ./cmd/server

test:
	go test ./...

test-race:
	go test -race -coverprofile=coverage.out ./...

fmt:
	gofmt -w $$(git ls-files '*.go')

run:
	go run ./cmd/server
