BINARY_NAME := unet.exe
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -ldflags "-X main.Version=$(VERSION)"
BUILD_DIR   := bin
GO          := go
GOFLAGS     :=

PLATFORMS   := linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64
MAX_SIZE_MB := 30

.PHONY: build dev test lint clean cross-build check-size install web-build web-dev

build: web-build
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/unet

web-build:
	cd web && npm run build

web-dev:
	cd web && npm run dev

dev:
	@$(MAKE) -j2 dev-go dev-web

dev-go:
	$(GO) run -race $(GOFLAGS) $(LDFLAGS) ./cmd/unet

dev-web:
	cd web && npm run dev

test: test-go test-web

test-go:
	$(GO) test -race ./...

test-web:
	cd web && npm test --if-present

lint: lint-go lint-web

lint-go:
	golangci-lint run ./...

lint-web:
	cd web && npm run lint

clean:
	rm -rf $(BUILD_DIR) web/dist web/node_modules

cross-build: web-build
	@mkdir -p $(BUILD_DIR)
	@set -e; \
	for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform##*/}; \
		echo "Building $(BINARY_NAME) for $$GOOS/$$GOARCH..."; \
		GOOS=$$GOOS GOARCH=$$GOARCH CGO_ENABLED=0 \
			$(GO) build $(GOFLAGS) $(LDFLAGS) \
			-o $(BUILD_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH ./cmd/unet; \
	done

check-size: cross-build
	@set -e; \
	for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform##*/}; \
		bin=$(BUILD_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		size=$$(stat -f%z "$$bin" 2>/dev/null || stat -c%s "$$bin" 2>/dev/null || wc -c < "$$bin"); \
		mb=$$((size / 1048576)); \
		if [ "$$size" -gt $$((MAX_SIZE_MB * 1048576)) ]; then \
			echo "FAIL: $$bin is $${mb}MB (limit: $(MAX_SIZE_MB)MB)"; \
			exit 1; \
		else \
			echo "OK:   $$bin is $${mb}MB"; \
		fi; \
	done

install: build
	cp $(BUILD_DIR)/$(BINARY_NAME) $(firstword $(shell $(GO) env GOPATH))/bin/$(BINARY_NAME)
