# Hooks Reference

This document provides a comprehensive reference for all hook types available in go-build-interceptor.

## Table of Contents

- [Overview](#overview)
- [Hook Types](#hook-types)
  - [Before/After Hooks](#beforeafter-hooks)
  - [Function Rewrite](#function-rewrite)
  - [Struct Modification](#struct-modification)
  - [File Generation](#file-generation)
- [Advanced Examples](#advanced-examples)
  - [Runtime Instrumentation (GLS)](#runtime-instrumentation-gls)
  - [Raw Code Injection via Rewrite](#raw-code-injection-via-rewrite)

## Overview

The hooks system provides several mechanisms for instrumenting Go code at compile time:

| Hook Type | Purpose | Use Case |
|-----------|---------|----------|
| Before/After | Inject calls before/after function execution | Tracing, logging, metrics |
| Rewrite | Complete AST transformation of a function | Signature changes, code injection |
| StructModification | Add fields to existing structs | Runtime context storage |
| GeneratedFile | Generate new source files into packages | Helper functions, accessors |

## Hook Types

### Before/After Hooks

The most common hook type. Injects function calls at the entry and exit points of target functions.

```go
package myhooks

import "github.com/pdelewski/go-build-interceptor/hooks"

type MyProvider struct{}

func (p *MyProvider) ProvideHooks() []*hooks.Hook {
    return []*hooks.Hook{
        {
            Target: hooks.InjectTarget{
                Package:  "net/http",
                Function: "ServeHTTP",
                Receiver: "serverHandler",
            },
            Hooks: &hooks.InjectFunctions{
                Before: "BeforeServeHTTP",
                After:  "AfterServeHTTP",
                From:   "github.com/myorg/myhooks",
            },
        },
    }
}
```

**Target Fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `Package` | Yes | The package containing the target function |
| `Function` | Yes | The function name to instrument |
| `Receiver` | No | For methods, the receiver type (e.g., `"*Server"` or `"Handler"`) |

**InjectFunctions Fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `Before` | No* | Name of the before hook function |
| `After` | No* | Name of the after hook function |
| `From` | Yes | Import path of the package containing hook implementations |

*At least one of `Before` or `After` must be specified.

**Hook Function Signature:**

```go
func BeforeServeHTTP(ctx hooks.HookContext) {
    // ctx provides access to function metadata
    fmt.Printf("Entering %s.%s\n", ctx.GetPackageName(), ctx.GetFuncName())
}

func AfterServeHTTP(ctx hooks.HookContext) {
    // Called after the original function returns
    fmt.Printf("Exiting %s.%s\n", ctx.GetPackageName(), ctx.GetFuncName())
}
```

**HookContext Interface:**

```go
type HookContext interface {
    SetData(data interface{})       // Store data for After hook
    GetData() interface{}           // Retrieve data from Before hook
    SetKeyData(key string, val interface{})
    GetKeyData(key string) interface{}
    HasKeyData(key string) bool
    SetSkipCall(skip bool)          // Skip original function (Before only)
    IsSkipCall() bool
    GetFuncName() string            // Target function name
    GetPackageName() string         // Target package name
}
```

---

### Function Rewrite

Provides complete control over a function's AST. Use this for advanced transformations like changing signatures, injecting arbitrary code, or completely replacing function bodies.

```go
import (
    "go/ast"
    "go/token"
    "github.com/pdelewski/go-build-interceptor/hooks"
)

func (p *MyProvider) ProvideHooks() []*hooks.Hook {
    return []*hooks.Hook{
        {
            Target: hooks.InjectTarget{
                Package:  "main",
                Function: "legacyFunction",
            },
            Rewrite: RewriteLegacyFunction,
        },
    }
}

func RewriteLegacyFunction(originalNode ast.Node) (ast.Node, error) {
    funcDecl, ok := originalNode.(*ast.FuncDecl)
    if !ok {
        return nil, fmt.Errorf("expected *ast.FuncDecl, got %T", originalNode)
    }

    // Example: Add a parameter
    funcDecl.Type.Params.List = append(funcDecl.Type.Params.List,
        &ast.Field{
            Names: []*ast.Ident{ast.NewIdent("ctx")},
            Type: &ast.SelectorExpr{
                X:   ast.NewIdent("context"),
                Sel: ast.NewIdent("Context"),
            },
        },
    )

    // Example: Inject code at the beginning
    logStmt := &ast.ExprStmt{
        X: &ast.CallExpr{
            Fun: &ast.SelectorExpr{
                X:   ast.NewIdent("fmt"),
                Sel: ast.NewIdent("Println"),
            },
            Args: []ast.Expr{
                &ast.BasicLit{Kind: token.STRING, Value: `"Function called"`},
            },
        },
    }
    funcDecl.Body.List = append([]ast.Stmt{logStmt}, funcDecl.Body.List...)

    return funcDecl, nil
}
```

**Combining Rewrite with Before/After:**

You can use both `Rewrite` and `Hooks` together. The rewrite is applied first, then hooks are injected.

```go
{
    Target: hooks.InjectTarget{
        Package:  "main",
        Function: "process",
    },
    Hooks: &hooks.InjectFunctions{
        Before: "BeforeProcess",
        After:  "AfterProcess",
        From:   "github.com/myorg/myhooks",
    },
    Rewrite: RewriteProcess,
}
```

---

### Struct Modification

Add new fields to existing struct definitions. Useful for storing instrumentation context within existing data structures.

```go
type StructField struct {
    Name string // Field name
    Type string // Field type as a string
}

type StructModification struct {
    Package    string        // Target package
    StructName string        // Name of the struct to modify
    AddFields  []StructField // Fields to add
}
```

**Example: Adding context fields to runtime.g**

```go
func GetStructModifications() []hooks.StructModification {
    return []hooks.StructModification{
        {
            Package:    "runtime",
            StructName: "g",
            AddFields: []hooks.StructField{
                {Name: "trace_context", Type: "interface{}"},
                {Name: "span_id", Type: "uint64"},
            },
        },
    }
}
```

**Note:** Struct modifications are typically processed by the build interceptor during the instrumentation phase, not at runtime.

---

### File Generation

Generate new source files to be compiled into target packages. Useful for adding helper functions, accessors, or initialization code.

```go
type GeneratedFile struct {
    Package  string // Target package
    FileName string // Name of the file to generate
    Content  string // The Go source code content
}
```

**Example: Generating accessor functions**

```go
func GetGeneratedFiles() []hooks.GeneratedFile {
    return []hooks.GeneratedFile{
        {
            Package:  "runtime",
            FileName: "context_accessors.go",
            Content: `package runtime

func GetTraceContext() interface{} {
    return getg().trace_context
}

func SetTraceContext(ctx interface{}) {
    getg().trace_context = ctx
}
`,
        },
    }
}
```

---

## Advanced Examples

### Runtime Instrumentation (GLS)

This example shows how to implement Goroutine Local Storage (GLS) for context propagation across goroutines. It combines all three advanced hook types.

```go
package runtime_instrumentation

import (
    "fmt"
    "go/ast"
    "go/parser"
    "go/token"

    "github.com/pdelewski/go-build-interceptor/hooks"
)

type RuntimeHookProvider struct{}

// 1. Struct Modification: Add fields to runtime.g
func (r *RuntimeHookProvider) GetStructModifications() []hooks.StructModification {
    return []hooks.StructModification{
        {
            Package:    "runtime",
            StructName: "g",
            AddFields: []hooks.StructField{
                {Name: "otel_trace_context", Type: "interface{}"},
                {Name: "otel_baggage_container", Type: "interface{}"},
            },
        },
    }
}

// 2. File Generation: Create accessor functions
func (r *RuntimeHookProvider) GetGeneratedFiles() []hooks.GeneratedFile {
    return []hooks.GeneratedFile{
        {
            Package:  "runtime",
            FileName: "runtime_gls.go",
            Content: `package runtime

func GetTraceContextFromGLS() interface{} {
    return getg().m.curg.otel_trace_context
}

func SetTraceContextToGLS(ctx interface{}) {
    getg().m.curg.otel_trace_context = ctx
}

func GetBaggageContainerFromGLS() interface{} {
    return getg().m.curg.otel_baggage_container
}

func SetBaggageContainerToGLS(ctx interface{}) {
    getg().m.curg.otel_baggage_container = ctx
}

type OtelContextCloner interface {
    Clone() interface{}
}

func propagateOtelContext(context interface{}) interface{} {
    if context == nil {
        return nil
    }
    if cloner, ok := context.(OtelContextCloner); ok {
        return cloner.Clone()
    }
    return context
}
`,
        },
    }
}

// 3. Rewrite Hook: Inject context propagation into newproc1
func (r *RuntimeHookProvider) ProvideHooks() []*hooks.Hook {
    return []*hooks.Hook{
        {
            Target: hooks.InjectTarget{
                Package:  "runtime",
                Function: "newproc1",
            },
            Rewrite: RewriteNewproc1,
        },
    }
}

func RewriteNewproc1(originalNode ast.Node) (ast.Node, error) {
    funcDecl, ok := originalNode.(*ast.FuncDecl)
    if !ok {
        return nil, fmt.Errorf("expected *ast.FuncDecl, got %T", originalNode)
    }

    // Rename unnamed return values so injected code can reference them
    renameReturnValues(funcDecl)

    // Inject context propagation as a defer statement
    rawCode := `defer func(){
        _unnamedRetVal0.otel_trace_context = propagateOtelContext(callergp.otel_trace_context)
        _unnamedRetVal0.otel_baggage_container = propagateOtelContext(callergp.otel_baggage_container)
    }()`

    stmts, err := parseSnippet(rawCode)
    if err != nil {
        return nil, err
    }

    funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
    return funcDecl, nil
}

// Helper: Rename unnamed return values to _unnamedRetVal0, _unnamedRetVal1, etc.
func renameReturnValues(funcDecl *ast.FuncDecl) {
    if funcDecl.Type.Results == nil {
        return
    }
    idx := 0
    for _, field := range funcDecl.Type.Results.List {
        if field.Names == nil {
            field.Names = []*ast.Ident{ast.NewIdent(fmt.Sprintf("_unnamedRetVal%d", idx))}
            idx++
        }
    }
}

// Helper: Parse a code snippet into AST statements
func parseSnippet(code string) ([]ast.Stmt, error) {
    wrapped := fmt.Sprintf("package p\nfunc f() {\n%s\n}", code)
    fset := token.NewFileSet()
    file, err := parser.ParseFile(fset, "", wrapped, 0)
    if err != nil {
        return nil, err
    }
    for _, decl := range file.Decls {
        if fn, ok := decl.(*ast.FuncDecl); ok {
            return fn.Body.List, nil
        }
    }
    return nil, fmt.Errorf("no function found")
}

var _ hooks.HookProvider = (*RuntimeHookProvider)(nil)
```

### Raw Code Injection via Rewrite

When you need to inject specific code (like a defer statement) without changing the function signature, use the Rewrite mechanism with AST parsing:

```go
func InjectDeferAtStart(originalNode ast.Node) (ast.Node, error) {
    funcDecl, ok := originalNode.(*ast.FuncDecl)
    if !ok {
        return nil, fmt.Errorf("expected *ast.FuncDecl")
    }

    // Parse the code to inject
    deferCode := `defer func() {
        if r := recover(); r != nil {
            log.Printf("Recovered: %v", r)
            panic(r) // re-panic after logging
        }
    }()`

    stmts, err := parseSnippet(deferCode)
    if err != nil {
        return nil, err
    }

    // Prepend to function body
    funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
    return funcDecl, nil
}
```

---

## Best Practices

1. **Use Before/After hooks** for simple tracing and logging - they're easier to maintain.

2. **Use Rewrite** when you need to:
   - Change function signatures
   - Inject code at specific locations (not just entry/exit)
   - Access or modify local variables
   - Reference function parameters or return values by name

3. **Use StructModification** sparingly - only when you need to store data within existing structures (like runtime.g for GLS).

4. **Use GeneratedFile** for helper functions that need to be in a specific package (like runtime) to access unexported symbols.

5. **Test rewrite functions** independently by parsing sample code and verifying the transformation.

6. **Handle errors gracefully** in rewrite functions - return meaningful error messages.