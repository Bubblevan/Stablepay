FROM golang:1.26.1-alpine AS builder

ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct \
    GO111MODULE=on

WORKDIR /workspace

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add --no-cache git ca-certificates

COPY go.mod go.sum* ./
COPY vendor ./vendor
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -o /out/api-gateway ./cmd/api-gateway

FROM alpine:latest

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/api-gateway /app/api-gateway
COPY configs /app/configs

EXPOSE 8080
ENTRYPOINT ["/app/api-gateway", "-config", "/app/configs/config.yaml"]
