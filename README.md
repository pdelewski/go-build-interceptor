# go-build-interceptor

A Go build instrumentation tool that injects hooks into functions without modifying source files. Add tracing, logging, or metrics to any Go code at build time.

## Installation

```bash
git clone https://github.com/pdelewski/go-build-interceptor
cd go-build-interceptor/hc
go build
```

The `hc` directory contains the **hook compiler** - the core tool that performs build-time instrumentation.

## Quick Start

### 1. Create a hooks file

```go
package myhooks

import (
    "fmt"
    "time"
    "github.com/pdelewski/go-build-interceptor/hooks"
)

func ProvideHooks() []*hooks.Hook {
    return []*hooks.Hook{
        {
            Target: hooks.InjectTarget{
                Package:  "main",
                Function: "myFunction",
            },
            Hooks: &hooks.InjectFunctions{
                Before: "BeforeMyFunction",
                After:  "AfterMyFunction",
                From:   "myhooks",
            },
        },
    }
}

func BeforeMyFunction(ctx hooks.HookContext) {
    ctx.SetKeyData("startTime", time.Now())
    fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

func AfterMyFunction(ctx hooks.HookContext) {
    if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
        fmt.Printf("[AFTER] %s.%s() took %v\n", ctx.GetPackageName(), ctx.GetFuncName(), time.Since(startTime))
    }
}
```

### 2. Compile with hooks

Navigate to your Go project directory, then compile with hooks passed as an argument:

```bash
cd /path/to/your/project
/path/to/hc --compile path/to/myhooks.go
# or short form
/path/to/hc -c path/to/myhooks.go
```

This builds your project with the hooks automatically injected.

### Real Examples

The project includes ready-to-use instrumentation examples:

#### Example 1: Function Tracing (hello)

`instrumentations/hello/generated_hooks.go` provides simple before/after tracing for functions in the `main` package. It logs function entry/exit with execution timing.

```bash
# Navigate to your hello example project
cd examples/hello

# Compile with tracing hooks
../../hc/hc -c ../../instrumentations/hello/generated_hooks.go

# Run the instrumented binary
./hello
```

Output:
```
[BEFORE] main.main()
[BEFORE] main.foo()
[AFTER] main.foo() completed in 1.002s
[BEFORE] main.bar1()
[AFTER] main.bar1() completed in 500ms
[AFTER] main.main() completed in 1.503s
```

#### Example 2: Runtime Instrumentation (GLS)

`instrumentations/runtime/runtime_hooks.go` provides Goroutine Local Storage (GLS) support by modifying Go's runtime. This enables context propagation across goroutines for distributed tracing.

**Important:** Runtime instrumentation is **required** if you need context to propagate between function calls and across goroutines. Without it, each function hook operates in isolation without access to shared trace context. If you only need simple function tracing without context propagation (like basic timing/logging), you can skip runtime hooks.

This example shows three hook capabilities:
- **Struct Modification**: Adds `otel_trace_context` and `otel_baggage_container` fields to the runtime `g` struct
- **File Generation**: Creates `runtime_gls.go` with accessor functions
- **Function Rewriting**: Injects context propagation into `newproc1`

```bash
# Compile with runtime GLS hooks only
./hc/hc -c ./instrumentations/runtime/runtime_hooks.go

# Compile with both runtime and application hooks (recommended for full tracing)
./hc/hc -c ./instrumentations/runtime/runtime_hooks.go,./instrumentations/hello/generated_hooks.go
```

#### Using Multiple Hooks Files

You can compile with multiple hooks files by specifying them comma-separated:

```bash
./hc/hc -c hooks1.go,hooks2.go,hooks3.go
```

Or use the UI file selector to pick multiple files interactively.

## Web UI

The included web UI provides an interactive environment for exploring code, generating hooks, and building.

![Web UI Screenshot](docs/images/ui-screenshot.png?v=2)

**UI Workflow Demo** - Generating hooks, compiling with instrumentation, and debugging the instrumented code:

![UI Workflow Demo](docs/images/go-build-interceptor.gif)

### Setup

**Linux/macOS:**
```bash
cd ui
make setup    # Install dependencies & build
make run      # Run with default project
```

**Windows:**
```cmd
cd ui
build.bat setup    # Install dependencies & build
build.bat run      # Run with default project
```

Or manually:

```bash
cd ui
npm install monaco-editor@0.45.0
cp -r node_modules/monaco-editor/min/vs static/monaco/
go build -o ui .
./ui -dir /path/to/your/project
```

Open http://localhost:9090 in your browser.

### UI Features

- **Code Editor** - Monaco editor with Go syntax highlighting and LSP support
- **Static Call Graph** - View function call relationships
- **Hook Generation** - Select functions and auto-generate hook code
- **Build & Run** - Compile with hooks and run directly from the UI

### Using the UI

1. **View Functions**: Click "Functions" in the View menu to see all functions in your project
2. **Generate Hooks**: Select functions with checkboxes, then click "Generate Hooks"
3. **Compile**: Click Run > Compile, select your hooks file(s), and build
4. **Run**: Click Run > Run Executable to test your instrumented binary

## Command Reference

| Command | Description |
|---------|-------------|
| `--compile <file>` / `-c <file>` | Build with hook instrumentation |
| `--capture` | Capture build commands to build-metadata/go-build.log |
| `--json` | Capture build with JSON output to build-metadata/ (recommended) |
| `--callgraph` | Show static call graph |
| `--pack-functions` | List all functions |
| `--pack-files` | List compiled files |

## Documentation

- [Hooks Reference](docs/hooks-reference.md) - Complete hook types and API
- [Architecture](docs/architecture.md) - Internal design and components

## Requirements

- Go 1.18+
- Linux, macOS, or Windows

## License

Apache License 2.0