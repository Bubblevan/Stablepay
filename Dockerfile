# StablePay Query Service
# 基于 Kitex 的微服务 - 查询服务

FROM golang:1.21-alpine AS builder

# 使用阿里云 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 设置 Go 模块代理为阿里云
ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GO111MODULE=on

# 安装构建依赖
RUN apk add --no-cache git

WORKDIR /build

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建可执行文件
RUN go build -o query-service .

# 运行时镜像
FROM alpine:latest

# 使用阿里云 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 安装运行时依赖（SQLite 需要）
RUN apk add --no-cache ca-certificates

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /build/query-service .

# 创建数据目录
RUN mkdir -p /data

# 暴露端口
EXPOSE 8084

# 数据卷（用于持久化 SQLite 数据库）
VOLUME ["/data"]

# 启动命令
CMD ["./query-service"]
