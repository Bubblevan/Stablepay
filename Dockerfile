# 在仓库根目录 StablePay 下构建（需要 payment-service 旁的 blockchain-adapter 以满足 go.mod replace）：
#   docker build -f payment-service/Dockerfile .
#
FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/golang:1.26.1-alpine AS builder

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GO111MODULE=on

RUN apk add --no-cache git

WORKDIR /src

COPY blockchain-adapter /src/blockchain-adapter
COPY payment-service /src/payment-service

WORKDIR /src/payment-service
RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o payment-service ./cmd/payment-service

FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/alpine:latest

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

RUN apk --no-cache add ca-certificates wget

RUN addgroup -g 1000 -S appgroup && \
    adduser -u 1000 -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /src/payment-service/payment-service .
COPY --from=builder /src/payment-service/config ./config

RUN chown -R appuser:appgroup /app

USER appuser

EXPOSE 8082 8888

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8082/health || exit 1

CMD ["./payment-service"]
