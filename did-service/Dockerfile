# StablePay DID Service
# 基于 Kitex 的微服务 - DID 身份服务

FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/golang:1.26.1-alpine AS builder

# 使用阿里云 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 设置 Go 模块代理为阿里云
ENV GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GO111MODULE=on

# 安装构建依赖
RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建可执行文件（使用 cmd/server/main.go 作为入口）
RUN go build -o did-service ./cmd/server/main.go

# 运行时镜像
FROM stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/alpine:latest

# 使用阿里云 Alpine 镜像源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 安装运行时依赖
RUN apk add --no-cache ca-certificates

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /build/did-service .

# 复制配置文件（如果有）
COPY --from=builder /build/conf ./conf

# 暴露端口
EXPOSE 8081

# 启动命令
CMD ["./did-service"]
