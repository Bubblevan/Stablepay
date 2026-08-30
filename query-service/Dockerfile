# StablePay Query Service
# 基于 Kitex 的微服务 - 查询服务

FROM golang:1.26.1-alpine AS builder

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GO111MODULE=on
ENV CGO_ENABLED=0

RUN apk add --no-cache git

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go clean -cache
RUN CGO_ENABLED=0 go build -o query-service ./cmd/server

FROM alpine:latest

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /build/query-service .

EXPOSE 8084

CMD ["./query-service"]
