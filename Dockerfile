# StablePay Blockchain Adapter
# 基于 Kitex 的微服务 - 区块链适配器

FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/golang:1.26.1-alpine AS builder

# 替换 Alpine 源为阿里云镜像（避免网络超时）
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 设置 Go 环境
ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GO111MODULE=on
ENV CGO_ENABLED=0

# 安装构建依赖（如果项目不需要 git，可删除此行）
RUN apk add --no-cache git

WORKDIR /build

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建可执行文件
RUN go build -o blockchain-adapter ./cmd/server/main.go

# 运行时镜像
FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/alpine:latest

# 替换 Alpine 源为阿里云镜像
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 安装运行时依赖
RUN apk add --no-cache ca-certificates

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /build/blockchain-adapter .

# 创建配置目录
RUN mkdir -p /app/conf

# 复制配置文件（如果有）
COPY --from=builder /build/conf ./conf

# 暴露端口
EXPOSE 8888

# 数据卷（用于热钱包密钥文件）
VOLUME ["/secure"]

# 启动命令
CMD ["./blockchain-adapter"]