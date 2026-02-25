.PHONY: all build clean install tidy vet

BINARY   := smarthomeentry-agent
BUILD_DIR := build
# Strip debug info and DWARF tables for a smaller production binary.
LDFLAGS  := -ldflags="-s -w"

all: build

## build: compile a static Linux/amd64 binary into ./build/
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/agent
	@echo "Built: $(BUILD_DIR)/$(BINARY)"

## build-arm64: cross-compile for Raspberry Pi 4 / arm64
build-arm64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-arm64 ./cmd/agent
	@echo "Built: $(BUILD_DIR)/$(BINARY)-arm64"

## build-arm: cross-compile for Raspberry Pi 3 and older / armv6
build-arm:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 \
		go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-arm ./cmd/agent
	@echo "Built: $(BUILD_DIR)/$(BINARY)-arm"

## install: build and install the binary to /usr/local/bin (requires root)
install: build
	install -o root -g root -m 755 $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)

## tidy: tidy and verify go.mod / go.sum
tidy:
	go mod tidy
	go mod verify

## vet: run go vet across all packages
vet:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
