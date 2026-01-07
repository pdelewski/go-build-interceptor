# Runtime Instrumentation

Advanced Go runtime instrumentation for Goroutine Local Storage (GLS) support.

## What it does

This instrumentation modifies Go's runtime to enable context propagation across goroutines. It's essential for distributed tracing where you need trace context to flow automatically when new goroutines are spawned.

## Components

### 1. Struct Modification

Adds two fields to the runtime `g` struct:
- `otel_trace_context` - For OpenTelemetry trace context
- `otel_baggage_container` - For OpenTelemetry baggage

### 2. File Generation

Creates `runtime_gls.go` with accessor functions:
- `GetTraceContextFromGLS()` / `SetTraceContextToGLS()`
- `GetBaggageContainerFromGLS()` / `SetBaggageContainerToGLS()`
- `propagateOtelContext()` - Handles context cloning

### 3. Function Rewriting

Injects context propagation into `runtime.newproc1` so that when a new goroutine is created, it inherits the trace context from its parent.

## Usage

```bash
# Runtime hooks only
./hc/hc -c ./instrumentations/runtime/runtime_hooks.go

# Combined with application hooks (recommended)
./hc/hc -c ./instrumentations/runtime/runtime_hooks.go,./instrumentations/hello/hello_hooks.go
```

## When to use

Use runtime instrumentation when you need:
- Trace context to propagate across goroutine boundaries
- Automatic context inheritance for new goroutines
- Distributed tracing with OpenTelemetry

Skip runtime instrumentation if you only need:
- Simple function entry/exit logging
- Timing measurements without context propagation

## Files

- `runtime_hooks.go` - Hook provider with struct modifications, file generation, and function rewriting
- `runtime_hooks_test.go` - Tests for the runtime hooks