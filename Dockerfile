# StablePay Verification Service
# 基于 Kitex：handler/main 依赖 kitex_gen（须提交到 Git；勿在 .gitignore 中排除）
# 本地生成：kitex -module verification-service -service verification_service <path>/verification-service.thrift

FROM golang:1.26.1-alpine AS builder

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
RUN go build -o verification-service .

# 运行时镜像
FROM alpine:latest

# 使用阿里云 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 安装运行时依赖
RUN apk add --no-cache ca-certificates wget

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /build/verification-service .

# 暴露端口
EXPOSE 8085

# 健康检查
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=5 \
    CMD wget -q --tries=1 -O /dev/null http://127.0.0.1:8085/health || exit 1

# 启动命令 - 设置端口为 8085
CMD ["./verification-service"]
