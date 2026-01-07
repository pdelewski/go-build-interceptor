# Examples

This directory contains example Go applications that can be instrumented with hc (hook compiler).

## Available Examples

| Example | Description |
|---------|-------------|
| [hello](hello/) | Simple application with nested function calls - ideal for learning basic instrumentation |
| [simple-http-server](simple-http-server/) | HTTP server with multiple endpoints - demonstrates instrumenting web handlers |

## Usage

Each example can be compiled with its corresponding instrumentation from the `instrumentations/` directory.

```bash
# Navigate to an example
cd examples/hello

# Compile with hooks
../../hc/hc -c ../../instrumentations/hello/hello_hooks.go

# Run the instrumented binary
./hello
```

## Directory Structure

Each example contains:
- `main.go` - The source code
- `go.mod` - Module definition
- `build-metadata/` - Generated build artifacts (after compilation)
- `.debug-build/` - Debug build files (after compilation)