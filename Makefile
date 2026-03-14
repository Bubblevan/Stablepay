.PHONY: help build server client test run-all clean

help:
	@echo "StablePay M0 - Makefile"
	@echo ""
	@echo "使用方式:"
	@echo "  make build       - 编译后端服务"
	@echo "  make server      - 启动后端服务"
	@echo "  make client      - 运行 Python 客户端"
	@echo "  make run-all     - 同时启动服务和客户端（需要两个终端）"
	@echo "  make test        - 运行测试"
	@echo "  make clean       - 清理编译产物"

build:
	@echo "编译后端服务..."
	cd cmd/server && go build -o ../../bin/server .
	@echo "? 编译完成: bin/server"

server: build
	@echo "启动后端服务..."
	./bin/server

client:
	@echo "运行客户端..."
	python3 cmd/client/main.py

run-all: build
	@echo "启动后端和客户端..."
	@echo "终端1 (后端): make server"
	@echo "终端2 (客户端): make client"

test:
	@echo "运行测试..."
	cd cmd/server && go test -v ./...

clean:
	@echo "清理..."
	rm -rf bin/
	@echo "? 清理完成"
