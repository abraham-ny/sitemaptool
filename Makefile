.PHONY: build test clean install dev release

# Build for current platform
build:
	go build -o smx main.go

# Build for all platforms
build-all:
	GOOS=linux GOARCH=amd64 go build -o smx-linux-amd64 main.go
	GOOS=linux GOARCH=arm64 go build -o smx-linux-arm64 main.go
	GOOS=darwin GOARCH=amd64 go build -o smx-darwin-amd64 main.go
	GOOS=darwin GOARCH=arm64 go build -o smx-darwin-arm64 main.go
	GOOS=windows GOARCH=amd64 go build -o smx-windows-amd64.exe main.go

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f smx smx-* *.exe

# Install locally
install:
	go install

# Development mode with hot reload (requires air)
dev:
	air

# Download dependencies
deps:
	go mod download
	go mod tidy

# Initialize module
init:
	go mod init github.com/abraham-ny/sitemaptool
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Show help
help:
	@echo "SitemapTool Makefile"
	@echo "===================="
	@echo "build       - Build for current platform"
	@echo "build-all   - Build for all platforms"
	@echo "test        - Run tests"
	@echo "clean       - Remove build artifacts"
	@echo "install     - Install binary locally"
	@echo "dev         - Run in development mode"
	@echo "deps        - Download dependencies"
	@echo "init        - Initialize Go module"
	@echo "fmt         - Format code"
	@echo "lint        - Lint code"
