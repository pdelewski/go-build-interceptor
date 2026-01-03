# Architecture

This document describes the internal architecture of go-build-interceptor.

## Overview

go-build-interceptor works by capturing the internal commands that `go build` executes during compilation. It parses these commands, analyzes the source code being compiled, and can inject instrumentation code (hooks) into target functions before the final compilation occurs.

## Core Components

### Build Capture System

The capture system runs `go build` with verbose flags (`-x -a -work`) and records all compilation commands. It supports two capture modes:

- **Text Capture**: Direct capture of build output to `go-build.log`
- **JSON Capture**: Captures structured JSON output from `go build -json`, then converts it to a processable text format

### Command Parser

The parser (`parser.go`) processes the captured build log, extracting individual compilation commands including:

- Compiler invocations with all arguments
- Linker commands
- File copy operations (heredoc-style commands)
- Environment variable settings (like WORK directory)

### Static Analyzer

The analyzer (`analyzer.go`) performs AST-based analysis of Go source files:

- Extracts function and method declarations with full signatures
- Identifies receivers, parameters, and return types
- Builds call graphs showing function relationships
- Filters analysis to current module packages only

### Hooks System

The hooks framework (`hooks/hooks.go`) provides a clean interface for defining function instrumentation.

```go
type Hook struct {
    Target  InjectTarget
    Hooks   *InjectFunctions    // For before/after hooks
    Rewrite FunctionRewriteHook // For complete function rewriting
}

type InjectTarget struct {
    Package  string
    Function string
    Receiver string
}

type InjectFunctions struct {
    Before string
    After  string
    From   string
}
```

See [Hooks Reference](hooks-reference.md) for complete documentation.

### Hooks Processor

The processor (`hooks_processor.go`) matches hook definitions against functions found in compilation units and performs the actual instrumentation:

- Parses hook definition files to extract targets
- Matches functions against hook specifications
- Injects trampoline function calls into matched functions
- Generates modified build logs for replay

## Build Interception Flow

1. **Capture Phase**: Run `go build -x -a -work -json` to capture all build commands
2. **Parse Phase**: Extract compile commands, identifying source files and packages
3. **Analysis Phase**: Parse Go source files to find function declarations
4. **Match Phase**: Compare functions against hook definitions
5. **Instrument Phase**: For matched functions, inject trampoline calls
6. **Replay Phase**: Execute modified build commands with instrumented sources

## Instrumentation Details

When a function matches a hook definition, the tool:

1. Creates a copy of the source file in the WORK directory
2. Parses the AST of the copied file
3. Injects a call to `trampoline_BeforeXXX()` at the function start
4. Wraps the function body with `defer trampoline_AfterXXX()` for cleanup
5. Adds trampoline function definitions that call the actual hooks
6. Updates the build commands to use the instrumented files

## Project Structure

```
go-build-interceptor/
├── main.go              # Entry point and main processing logic
├── parser.go            # Build log parser
├── analyzer.go          # AST-based code analyzer
├── capture.go           # Build output capture
├── config.go            # Configuration and flag parsing
├── types.go             # Shared type definitions
├── hooks_processor.go   # Hook matching and instrumentation
├── hooks/
│   └── hooks.go         # Hook framework definitions
├── ui/
│   ├── web_main.go      # Web UI server with LSP proxy
│   ├── go.mod           # UI module dependencies
│   ├── Makefile         # Build automation
│   └── static/
│       ├── editor.js    # Monaco editor integration + LSP client
│       ├── editor.css   # Editor styles
│       └── monaco/      # Monaco Editor files (via npm)
└── examples/            # Example applications
```

## Generated Files

During operation, the tool creates several files:

| File | Description |
|------|-------------|
| `go-build.log` | Captured build commands (text format) |
| `go-build.json` | Raw JSON build output (when using --json) |
| `go-build-modified.log` | Build log with paths updated for instrumented files |
| `replay_script.sh` | Executable bash script to replay the build |

## Hook Context

The `HookContext` interface provides information about the hooked function call:

```go
type HookContext interface {
    SetData(data interface{})
    GetData() interface{}
    SetKeyData(key string, val interface{})
    GetKeyData(key string) interface{}
    HasKeyData(key string) bool
    SetSkipCall(skip bool)
    IsSkipCall() bool
    GetFuncName() string
    GetPackageName() string
}
```

## Function Rewriting

For advanced use cases, you can completely rewrite a function's AST:

```go
{
    Target: hooks.InjectTarget{
        Package:  "main",
        Function: "legacyFunction",
        Receiver: "",
    },
    Rewrite: func(originalNode ast.Node) (ast.Node, error) {
        funcDecl, ok := originalNode.(*ast.FuncDecl)
        if !ok {
            return nil, fmt.Errorf("expected *ast.FuncDecl")
        }
        // Modify the function declaration as needed
        return funcDecl, nil
    },
}
```

## Limitations

- Currently supports before/after style hooks; more complex interception patterns require function rewriting
- Static call graph analysis may not capture all dynamic dispatch scenarios
- Some edge cases in Go's build system may not be fully captured