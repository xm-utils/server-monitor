BINARY_SERVER := server-monitor
BINARY_ADMIN := admin-monitor
BUILD_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PKG_SERVER := server_monitor_service/cmd/server-monitor
PKG_ADMIN := server_monitor_service/cmd/admin-monitor
LDFLAGS := -ldflags "-s -w -X server_monitor_service/internal/server.buildVersion=$(VERSION)"

.PHONY: all build build-server build-admin build-linux build-linux-server build-linux-admin run-server run-admin proto tidy clean test

all: build

# 本机构建
build: build-server build-admin

build-server:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER) $(PKG_SERVER)

build-admin:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ADMIN) $(PKG_ADMIN)

# 交叉编译 Linux 静态二进制
build-linux: build-linux-server build-linux-admin

build-linux-server:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER)-linux-amd64 $(PKG_SERVER)

build-linux-admin:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ADMIN)-linux-amd64 $(PKG_ADMIN)

# 运行
run-server:
	go run $(PKG_SERVER) -config ./configs/server.example.yaml

run-admin:
	go run $(PKG_ADMIN) -config ./configs/admin.example.yaml

# Proto 生成
proto:
	protoc --proto_path=internal/protocol/pb \
		--go_out=internal/protocol/pb --go_opt=paths=source_relative \
		--go-grpc_out=internal/protocol/pb --go-grpc_opt=paths=source_relative \
		internal/protocol/pb/monitor.proto

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -rf $(BUILD_DIR)
