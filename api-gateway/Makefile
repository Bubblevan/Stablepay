APP_NAME=api-gateway

.PHONY: tidy test run build

tidy:
	go mod tidy

test:
	go test ./...

run:
	go run ./cmd/api-gateway -config configs/config.yaml

build:
	go build -o bin/$(APP_NAME) ./cmd/api-gateway
