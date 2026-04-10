# StablePay Query Service
# 基于 Kitex 的微服务 - 查询服务

FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/golang:1.26.1-alpine AS builder

# 使用阿里云 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 设置 Go 模块代理为阿里云
ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GO111MODULE=on
ENV CGO_ENABLED=0

# 安装构建依赖（纯 Go 构建不需要 sqlite/sqlite-libs 或 gcc）
RUN apk add --no-cache git

WORKDIR /build

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建可执行文件（禁用 cgo，避免 SQLite CGO 依赖）
RUN go clean -cache
RUN CGO_ENABLED=0 go build -o query-service .

# 运行时镜像
FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/alpine:latest

# 使用阿里云 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 安装运行时依赖
RUN apk add --no-cache ca-certificates

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /build/query-service .

# 暴露端口：8084 Kitex RPC，8184 HTTP adapter（供 api-gateway 调用）
EXPOSE 8084 8184

# 启动命令
CMD ["./query-service"]
