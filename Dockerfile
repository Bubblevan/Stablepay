# syntax=docker/dockerfile:1.7

FROM golang:1.26.1-alpine AS builder

ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct \
    GO111MODULE=on \
    GOPRIVATE=code.wenfu.cn,codeup.aliyun.com \
    GONOSUMDB=code.wenfu.cn,codeup.aliyun.com

WORKDIR /workspace

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add --no-cache git openssh-client ca-certificates && \
    mkdir -p /root/.ssh && chmod 700 /root/.ssh && \
    ssh-keyscan codeup.aliyun.com >> /root/.ssh/known_hosts

COPY go.mod go.sum* ./
RUN --mount=type=ssh go mod download

COPY . .
RUN --mount=type=ssh CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api-gateway ./cmd/api-gateway

FROM alpine:latest

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/api-gateway /app/api-gateway
COPY configs /app/configs

EXPOSE 8080
ENTRYPOINT ["/app/api-gateway", "-config", "/app/configs/config.yaml"]