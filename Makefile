.PHONY: all build test test-cover lint install clean run

# Build variables
BINARY := pr-monitor
BUILD_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Default target
all: lint test build

# Build the binary
build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/pr-monitor

# Run all tests
test:
	go test ./...

# Run tests with coverage
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Run linting
lint:
	go vet ./...

# Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/pr-monitor

# Run locally
run: build
	./$(BUILD_DIR)/$(BINARY)

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR) coverage.out
