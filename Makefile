.PHONY: build test clean run docker

# 变量
APP_NAME=payment-service
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell go version | awk '{print $$3}')

# 编译标志
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GoVersion=$(GO_VERSION)"

# 默认目标
all: build

# 构建
build:
	go build $(LDFLAGS) -o bin/$(APP_NAME) cmd/$(APP_NAME)/main.go

# 测试
test:
	go test -v -race -coverprofile=coverage.out ./...

# 测试覆盖率
coverage: test
	go tool cover -html=coverage.out -o coverage.html

# 清理
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# 运行
run:
	go run cmd/$(APP_NAME)/main.go

# 格式化代码
fmt:
	go fmt ./...

# 代码检查
lint:
	golangci-lint run

# 下载依赖
deps:
	go mod download
	go mod tidy

# 生成代码（如使用 thriftgo）
generate:
	# thriftgo -g go -o ./idl idl/payment-service.thrift

# Docker 构建
docker:
	docker build -t $(APP_NAME):$(VERSION) .

# Docker 推送
docker-push:
	docker tag $(APP_NAME):$(VERSION) $(DOCKER_REGISTRY)/$(APP_NAME):$(VERSION)
	docker push $(DOCKER_REGISTRY)/$(APP_NAME):$(VERSION)

# 本地开发环境启动
dev-up:
	docker-compose -f docker-compose.dev.yml up -d

# 本地开发环境停止
dev-down:
	docker-compose -f docker-compose.dev.yml down

# 数据库迁移
migrate:
	mysql -u root -p < scripts/init_db.sql

# 帮助
help:
	@echo "Available targets:"
	@echo "  build       - Build the application"
	@echo "  test        - Run tests"
	@echo "  coverage    - Generate test coverage report"
	@echo "  clean       - Clean build artifacts"
	@echo "  run         - Run the application"
	@echo "  fmt         - Format code"
	@echo "  lint        - Run linter"
	@echo "  deps        - Download dependencies"
	@echo "  docker      - Build Docker image"
	@echo "  dev-up      - Start local development environment"
	@echo "  dev-down    - Stop local development environment"
	@echo "  migrate     - Run database migrations"
