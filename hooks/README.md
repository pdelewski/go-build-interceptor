# Hooks Framework

A lean Go DSL for defining code injection hooks. This framework allows you to specify pairs of functions that should be injected into arbitrary Go code for instrumentation, monitoring, or other cross-cutting concerns.

## Core Concept

The framework follows a simple principle: **"Take these two functions from here and inject them into this target"**

```go
hook := &Hook{
    Target: InjectTarget{
        Package:  "net/http",      // Where to inject
        Function: "ServeHTTP",     // Which function to wrap  
        Receiver: "serverHandler", // Which receiver type
    },
    Hooks: InjectFunctions{
        Before: "BeforeServeHTTP", // Function to call before
        After:  "AfterServeHTTP",  // Function to call after
        From:   "github.com/yourorg/instrumentation/nethttp/server", // Where these functions live
    },
}
```

## Hook Function Signatures

All hook implementations must follow these signatures:

```go
type BeforeHook func(hookCtx *HookContext) error
type AfterHook func(hookCtx *HookContext) error
```

The `HookContext` provides all necessary information:

```go
type HookContext struct {
    Package  string        // Target package being instrumented
    Function string        // Target function being instrumented  
    Receiver string        // Target receiver type
    Args     []interface{} // Function arguments
    StartTime time.Time    // When the function started
    
    // For After hooks only
    Result   interface{}   // Function return value
    Error    error         // Any error that occurred
    Duration time.Duration // How long the function took
    
    Ctx context.Context    // User context
}
```

## Usage Examples

### HTTP Server Instrumentation

```go
// In your instrumentation package
func BeforeServeHTTP(ctx *HookContext) error {
    w := ctx.Args[0].(http.ResponseWriter)
    req := ctx.Args[1].(*http.Request)
    
    fmt.Printf("Starting HTTP request: %s %s\n", req.Method, req.URL.Path)
    return nil
}

func AfterServeHTTP(ctx *HookContext) error {
    req := ctx.Args[1].(*http.Request)
    fmt.Printf("Completed HTTP request in %v: %s %s\n", 
        ctx.Duration, req.Method, req.URL.Path)
    return nil
}

// Hook definition
hook := hooks.HTTPServerHook(
    "BeforeServeHTTP",
    "AfterServeHTTP", 
    "github.com/yourorg/instrumentation/nethttp/server",
)
```

### Database Instrumentation

```go
hook := hooks.SQLHook(
    "BeforeQuery",
    "AfterQuery",
    "github.com/yourorg/instrumentation/sql",
)
```

### Custom Business Logic

```go
hook := hooks.CustomHook(
    "github.com/myapp/orders",  // target package
    "ProcessOrder",             // target function
    "OrderService",             // target receiver
    "BeforeProcessOrder",       // before hook function
    "AfterProcessOrder",        // after hook function  
    "github.com/myorg/instrumentation/orders", // hook package
)
```

### Registry for Multiple Hooks

```go
registry := hooks.NewRegistry().
    MustAdd(hooks.HTTPServerHook("BeforeServeHTTP", "AfterServeHTTP", "...")).
    MustAdd(hooks.SQLHook("BeforeQuery", "AfterQuery", "...")).
    MustAdd(hooks.CustomHook("pkg", "func", "recv", "before", "after", "..."))

// Get all hooks for processing by code injector
allHooks := registry.GetHooks()
```

## Implementation Template

When creating new hook functions, use this template:

```go
package your_instrumentation

import "github.com/yourorg/go-build-interceptor/hooks"

// BeforeTargetFunction implements hooks.BeforeHook
func BeforeTargetFunction(ctx *hooks.HookContext) error {
    // Cast arguments to expected types
    arg0 := ctx.Args[0].(ExpectedType)
    arg1 := ctx.Args[1].(AnotherType)
    
    // Your before logic here
    fmt.Printf("Starting %s.%s\n", ctx.Receiver, ctx.Function)
    
    return nil
}

// AfterTargetFunction implements hooks.AfterHook  
func AfterTargetFunction(ctx *hooks.HookContext) error {
    // Access return value if function returns something
    if ctx.Result != nil {
        result := ctx.Result.(ExpectedReturnType)
        // Use result...
    }
    
    // Your after logic here
    fmt.Printf("Completed %s.%s in %v\n", 
        ctx.Receiver, ctx.Function, ctx.Duration)
    
    if ctx.Error != nil {
        fmt.Printf("Function failed: %v\n", ctx.Error)
    }
    
    return nil
}
```

## Code Generation

The framework defines the hooks, but actual code injection is handled by a separate code generation/AST manipulation component. The generated code would look like:

```go
// Original function:
func (sh serverHandler) ServeHTTP(w ResponseWriter, r *Request) {
    // original body
}

// After injection:
func (sh serverHandler) ServeHTTP(w ResponseWriter, r *Request) {
    ctx := &HookContext{
        Package: "net/http",
        Function: "ServeHTTP", 
        Receiver: "serverHandler",
        Args: []interface{}{w, r},
        StartTime: time.Now(),
    }
    
    if err := instrumentation.BeforeServeHTTP(ctx); err != nil {
        // handle error
    }
    
    defer func() {
        ctx.Duration = time.Since(ctx.StartTime)
        if r := recover(); r != nil {
            ctx.Error = fmt.Errorf("panic: %v", r)
        }
        if err := instrumentation.AfterServeHTTP(ctx); err != nil {
            // handle error  
        }
    }()
    
    // original function body
}
```

## Validation

All hooks are validated when added to the registry:

- Target package, function, and receiver must be specified
- Before and after function names must be provided  
- Instrumentation package path must be valid

```go
if err := hook.Validate(); err != nil {
    // Handle validation error
}
```