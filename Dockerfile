# StablePay DID Service
# 基于 Kitex 的微服务 - DID 身份服务

FROM golang:1.21-alpine AS builder

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
FROM alpine:latest

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
