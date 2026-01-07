# Hello Example

A minimal Go application demonstrating basic function instrumentation with hc.

## What it does

This example contains a simple call chain: `main()` -> `foo()` -> `bar1()` -> `bar2()`

It's designed to demonstrate:
- Function entry/exit tracing
- Execution timing measurement
- Nested function call instrumentation

## Building with instrumentation

```bash
# From this directory
../../hc/hc -c ../../instrumentations/hello/hello_hooks.go

# Run the instrumented binary
./hello
```

## Expected output

```
[BEFORE] main.main()
hello
[BEFORE] main.foo()
[BEFORE] main.bar1()
[BEFORE] main.bar2()
[AFTER] main.bar2() completed in 125ns
[AFTER] main.bar1() completed in 42.583µs
[AFTER] main.foo() completed in 82.75µs
[AFTER] main.main() completed in 1.234ms
```

## With runtime instrumentation (GLS)

To enable context propagation across goroutines:

```bash
../../hc/hc -c ../../instrumentations/runtime/runtime_hooks.go,../../instrumentations/hello/hello_hooks.go
./hello
```