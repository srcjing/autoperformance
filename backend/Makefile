# auto_performance 后端 Makefile

.PHONY: build run test vet tidy clean

BINARY := bin/auto_performance

## 编译后端单二进制（内嵌前端 + 模板）
build:
	go build -o $(BINARY) ./cmd/server

## 本地运行
run:
	go run ./cmd/server

## 运行测试
test:
	go test ./...

## 静态检查
vet:
	go vet ./...

## 整理依赖
tidy:
	go mod tidy

## 清理构建产物
clean:
	rm -rf bin
