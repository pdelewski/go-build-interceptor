# Hello Hook Module

This module provides `HookProvider` implementations for instrumenting the `hello` module functions using the go-build-interceptor hooks framework.

## Features

- **Basic Hooks**: Simple before/after hooks for all functions in the hello module
- **Tracing Hooks**: Advanced call tracing with indentation to show call hierarchy
- Full implementation of the `HookProvider` interface

## Usage

### Basic Hook Provider

```go
import "github.com/pdelewski/go-build-interceptor/examples/hello/hello_hook"

provider := &hello_hook.HelloHookProvider{}
hooks := provider.ProvideHooks()

// Register hooks with your instrumentation system
for _, hook := range hooks {
    // Process each hook definition
    fmt.Printf("Hook for %s.%s\n", hook.Target.Package, hook.Target.Function)
}
```

### Tracing Hook Provider

```go
import "github.com/pdelewski/go-build-interceptor/examples/hello/hello_hook"

provider := hello_hook.NewTracingHookProvider()
hooks := provider.ProvideHooks()

// When instrumented code runs, it will output:
// → main()
//   → foo()
//     → bar1()
//       → bar2()
//       ← bar2() [100ns]
//     ← bar1() [250ns]
//   ← foo() [500ns]
// ← main() [1ms]
```

## Hooked Functions

The module provides hooks for the following functions from the `hello` module:

1. `main()` - The entry point
2. `foo()` - Called from main
3. `bar1()` - Called from foo
4. `bar2()` - Called from bar1

## Hook Implementations

### Basic Hooks
- `BeforeFoo` / `AfterFoo` - Simple logging of function entry/exit
- `BeforeBar1` / `AfterBar1` - Simple logging of function entry/exit
- `BeforeBar2` / `AfterBar2` - Simple logging of function entry/exit
- `BeforeMain` / `AfterMain` - Simple logging with error handling

### Tracing Hooks
- `TracingBefore*` / `TracingAfter*` - Maintains call depth and outputs hierarchical trace
- Thread-safe implementation using mutex
- Provides `GetCallTrace()` to retrieve the call sequence
- Provides `ResetCallTrace()` to clear the trace

## Example Output

When the hello program is instrumented with basic hooks:
```
[main] Starting main()
hello
[foo] Starting foo()
[bar1] Starting bar1()
[bar2] Starting bar2()
[bar2] Completed bar2() in 50ns
[bar1] Completed bar1() in 150ns
[foo] Completed foo() in 300ns
[main] Completed main() in 1ms
```

When instrumented with tracing hooks:
```
→ main()
hello
  → foo()
    → bar1()
      → bar2()
      ← bar2() [50ns]
    ← bar1() [150ns]
  ← foo() [300ns]
← main() [1ms]
```

## Testing

Run the tests with:
```bash
cd hello_hook
go test -v
```

## Integration

This module is designed to work with the go-build-interceptor's compile-time instrumentation system. The hook definitions provided by the `HookProvider` implementation will be used to inject the instrumentation code during the build process.