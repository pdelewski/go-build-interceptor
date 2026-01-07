# Instrumentations

This directory contains hook definitions for instrumenting Go applications with hc (hook compiler).

## Available Instrumentations

| Instrumentation | Description |
|-----------------|-------------|
| [hello](hello/) | Function tracing hooks for the hello example |
| [simple-http-server](simple-http-server/) | HTTP handler tracing for the simple-http-server example |
| [runtime](runtime/) | Go runtime instrumentation for Goroutine Local Storage (GLS) |

## Types of Hooks

### Application Hooks (hello, simple-http-server)

These provide before/after function tracing:
- Log function entry and exit
- Measure execution time
- Pass data between before and after hooks

### Runtime Hooks

The runtime instrumentation enables advanced features:
- **Struct Modification** - Adds fields to Go's internal `g` struct
- **File Generation** - Creates accessor functions in the runtime package
- **Function Rewriting** - Injects context propagation into `newproc1`

## Usage

```bash
# Single instrumentation
./hc/hc -c ./instrumentations/hello/hello_hooks.go

# Multiple instrumentations (comma-separated)
./hc/hc -c ./instrumentations/runtime/runtime_hooks.go,./instrumentations/hello/hello_hooks.go
```

## Creating Custom Hooks

See the [Hooks Reference](../docs/hooks-reference.md) for complete documentation on creating your own hook definitions.