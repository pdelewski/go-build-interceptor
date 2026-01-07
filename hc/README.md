# hc - Hook Compiler

The hook compiler (hc) is the core component of go-build-interceptor that performs build-time instrumentation of Go code.

## What it does

hc intercepts the Go build process to inject hooks into functions without modifying source files. It works by:

1. Capturing build commands from `go build`
2. Parsing and analyzing the source code
3. Injecting instrumentation based on hook definitions
4. Replaying the modified build

## Files

| File | Description |
|------|-------------|
| `main.go` | Entry point and main processing logic |
| `parser.go` | Build log parser - extracts compilation commands |
| `analyzer.go` | AST-based code analyzer - extracts functions and call graphs |
| `capture.go` | Build output capture - runs `go build` and captures commands |
| `config.go` | Configuration and command-line flag parsing |
| `types.go` | Shared type definitions |
| `hooks_processor.go` | Hook matching and instrumentation injection |

## Building

```bash
cd hc
go build
```

## Usage

```bash
# Compile with hook instrumentation
./hc -c path/to/hooks.go

# Capture build commands
./hc --json

# Show static call graph
./hc --callgraph

# List all functions
./hc --pack-functions
```

See the main [README](../README.md) for full documentation.