# ============================================================
# Stage 1: Build Go binary
# ============================================================
FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/golang:1.26.1-alpine AS builder

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GO111MODULE=on
ENV CGO_ENABLED=0

WORKDIR /build

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o merchant-server ./cmd/merchant-server/

# ============================================================
# Stage 2: Minimal runtime image
# ============================================================
FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/alpine:latest

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

RUN apk add --no-cache tzdata ca-certificates wget \
    && mkdir -p /app/data /app/config \
    && adduser -D -h /app merchant

COPY --from=builder /build/merchant-server /app/merchant-server
COPY --from=builder /build/config/config.yaml /app/config/config.yaml

# 非 root 运行
USER merchant
WORKDIR /app

HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --tries=1 -O /dev/null http://127.0.0.1:8787/healthz || exit 1

EXPOSE 8787

CMD ["/app/merchant-server"]
