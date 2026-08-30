.PHONY: help gen gen-clean check-tools verify-contract verify-services

help:
	@echo "stablepayai-idl targets:"
	@echo "  make gen           # 使用 thriftgo 生成 Go 类型到 ./gen-go（用于验证/联调）"
	@echo "  make gen-clean     # 清理生成目录"
	@echo "  make check-tools   # 检查 thriftgo 是否可用"
	@echo "  make verify-contract # 校验 canonical IDL、兼容副本和当前生成接口"

check-tools:
	@command -v thriftgo >/dev/null 2>&1 || (echo "未找到 thriftgo，请先安装 thriftgo。" && exit 1)
	@echo "thriftgo: OK"

verify-contract:
	@powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/verify-contract.ps1

verify-services:
	@powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/verify-service-contracts.ps1

gen: check-tools
	@./scripts/generate.sh ./gen-go

gen-clean:
	@rm -rf ./gen-go ./gen

