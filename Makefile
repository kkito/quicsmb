MODULE := quicsmb
BIN := quicsmb
DIST := dist
GOFLAGS ?=

.PHONY: all build build-all test test-front test-all vet clean web-build e2e

all: build

web-build: ## 打包前端（esbuild → build/web-dist + internal/webdist embed）
	NODE_PATH=$$(npm root -g) node scripts/build_web.cjs

build: web-build
	go build $(GOFLAGS) -o $(DIST)/$(BIN) ./cmd/quicsmb

build-all: ## 交叉编译 darwin-arm64 + linux-amd64
	mkdir -p $(DIST)
	GOOS=darwin GOARCH=arm64 go build -o $(DIST)/$(BIN)-darwin-arm64 ./cmd/quicsmb
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(DIST)/$(BIN)-linux-amd64 ./cmd/quicsmb
	ls -lh $(DIST)

test:
	go test -count=1 -v ./...

test-front: ## 前端单元测试（vitest + happy-dom，全局工具链）
	NODE_PATH=$$(npm root -g) vitest run --root web

test-all: ## 全部测试（go test + vitest）
	go test -count=1 ./...
	NODE_PATH=$$(npm root -g) vitest run --root web

vet:
	go vet ./...

clean:
	rm -rf $(DIST) build

e2e: build ## 本地回环：假 SMB 后端 + server + client + 发包验证
	./scripts/e2e.sh
