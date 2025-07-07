.PHONY: build install clean test fmt vet

# Binary name
BINARY_NAME=tapio
INSTALL_PATH=/usr/local/bin

# Build variables
BUILD_DIR=./build
MAIN_PATH=./cmd/tapio/main.go
GO=go
GOFLAGS=-ldflags="-s -w"

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Install the binary to system path
install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_PATH)..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	@sudo chmod +x $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "Installation complete!"
	@echo "Run '$(BINARY_NAME) --help' to get started"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

# Development build (with debug symbols)
dev:
	@echo "Building $(BINARY_NAME) (development mode)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Development build complete: $(BUILD_DIR)/$(BINARY_NAME)"