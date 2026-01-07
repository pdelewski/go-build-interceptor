# Hello Instrumentation

Hook definitions for tracing the hello example application.

## What it does

Provides before/after hooks for all functions in the hello example:
- `main()`
- `foo()`
- `bar1()`
- `bar2()`

Each hook:
- Records the start time before function execution
- Logs function entry with package and function name
- Logs function exit with execution duration

## Usage

```bash
cd examples/hello
../../hc/hc -c ../../instrumentations/hello/hello_hooks.go
./hello
```

## Hook Pattern

Each function gets a pair of hooks:

```go
func BeforeFoo(ctx hooks.HookContext) {
    ctx.SetKeyData("startTime", time.Now())
    fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

func AfterFoo(ctx hooks.HookContext) {
    if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
        duration := time.Since(startTime)
        fmt.Printf("[AFTER] %s.%s() completed in %v\n",
            ctx.GetPackageName(), ctx.GetFuncName(), duration)
    }
}
```

## Files

- `hello_hooks.go` - Hook definitions and implementations
- `hello_hooks_test.go` - Tests for the hooks