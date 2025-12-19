.PHONY: all build clean setup monaco deps run help

# Default target
all: build

# Build the UI server
build: monaco
	go build -o ui .

# Setup everything from scratch
setup: deps monaco build

# Install npm dependencies
deps:
	@echo "Installing npm dependencies..."
	npm install monaco-editor@0.45.0

# Copy Monaco Editor to static directory
monaco: deps
	@echo "Setting up Monaco Editor..."
	@mkdir -p static/monaco
	@cp -r node_modules/monaco-editor/min/vs static/monaco/
	@echo "Monaco Editor installed to static/monaco/"

# Run the UI server (default: hello project)
run: build
	./ui -dir ../hello

# Run with custom directory
# Usage: make run-dir DIR=/path/to/project
run-dir: build
	./ui -dir $(DIR)

# Clean build artifacts
clean:
	rm -f ui
	rm -rf static/monaco

# Deep clean (including node_modules)
clean-all: clean
	rm -rf node_modules
	rm -f package.json package-lock.json

# Install gopls if not present
gopls:
	@which gopls > /dev/null || (echo "Installing gopls..." && go install golang.org/x/tools/gopls@latest)

# Help
help:
	@echo "UI Build Targets:"
	@echo "  make setup     - Full setup (deps + monaco + build)"
	@echo "  make build     - Build the UI server"
	@echo "  make deps      - Install npm dependencies"
	@echo "  make monaco    - Copy Monaco to static directory"
	@echo "  make run       - Build and run with ../hello project"
	@echo "  make run-dir DIR=/path  - Run with custom project"
	@echo "  make gopls     - Install gopls if missing"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make clean-all - Remove all generated files"
	@echo "  make help      - Show this help"
