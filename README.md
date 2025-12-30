# go-build-interceptor

A powerful Go build instrumentation tool that captures, analyzes, and modifies the Go compilation process. This tool intercepts the Go build system to enable function-level code instrumentation, call graph analysis, and build replay capabilities.

## Overview

go-build-interceptor works by capturing the internal commands that `go build` executes during compilation. It parses these commands, analyzes the source code being compiled, and can inject instrumentation code (hooks) into target functions before the final compilation occurs.

The tool is particularly useful for:

- Adding observability (tracing, logging, metrics) to existing Go code without modifying source files
- Understanding code flow through static call graph analysis
- Debugging and profiling by injecting before/after hooks into functions
- Build system analysis and replay
- Automated code instrumentation for testing or monitoring

## Architecture

The project consists of several core components:

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

The hooks framework (`hooks/hooks.go`) provides a clean interface for defining function instrumentation:

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

### Hooks Processor

The processor (`hooks_processor.go`) matches hook definitions against functions found in compilation units and performs the actual instrumentation:

- Parses hook definition files to extract targets
- Matches functions against hook specifications
- Injects trampoline function calls into matched functions
- Generates modified build logs for replay

## Installation

```bash
go get github.com/pdelewski/go-build-interceptor
```

Or clone and build locally:

```bash
git clone https://github.com/pdelewski/go-build-interceptor
cd go-build-interceptor
go build
```

## Usage

### Basic Commands

```bash
# Capture build output (text format)
./go-build-interceptor --capture

# Capture build output (JSON format - recommended)
./go-build-interceptor --json

# Generate replay script from captured build log
./go-build-interceptor --log go-build.log

# Execute the replay script
./go-build-interceptor --log go-build.log --execute

# Interactive execution (step through commands)
./go-build-interceptor --log go-build.log --interactive
```

### Analysis Commands

```bash
# Display parsed commands verbosely
./go-build-interceptor --log go-build.log --verbose

# Dump raw command information
./go-build-interceptor --log go-build.log --dump

# Dry run - show commands without executing
./go-build-interceptor --log go-build.log --dry-run

# List files from compile commands
./go-build-interceptor --log go-build.log --pack-files

# Extract function definitions from compiled files
./go-build-interceptor --log go-build.log --pack-functions

# List package names from compile commands
./go-build-interceptor --log go-build.log --pack-packages

# Show package names with source paths
./go-build-interceptor --log go-build.log --pack-packagepath

# Generate call graph from compiled files
./go-build-interceptor --log go-build.log --callgraph

# Inspect the WORK directory contents
./go-build-interceptor --log go-build.log --workdir
```

### Compilation with Hooks

```bash
# Compile with hook instrumentation
./go-build-interceptor --compile path/to/hooks.go
# or short form
./go-build-interceptor -c path/to/hooks.go
```

## Creating Hook Definitions

Hook definitions are Go files that implement the `HookProvider` interface. Here is a complete example:

```go
package myhooks

import (
    "fmt"
    "github.com/pdelewski/go-build-interceptor/hooks"
)

type MyHookProvider struct{}

func (h *MyHookProvider) ProvideHooks() []*hooks.Hook {
    return []*hooks.Hook{
        {
            Target: hooks.InjectTarget{
                Package:  "main",
                Function: "ProcessRequest",
                Receiver: "",  // Empty for package-level functions
            },
            Hooks: &hooks.InjectFunctions{
                Before: "BeforeProcessRequest",
                After:  "AfterProcessRequest",
                From:   "github.com/myorg/myhooks",
            },
        },
        {
            Target: hooks.InjectTarget{
                Package:  "main",
                Function: "Handle",
                Receiver: "*Server",  // For methods, specify the receiver type
            },
            Hooks: &hooks.InjectFunctions{
                Before: "BeforeHandle",
                After:  "AfterHandle",
                From:   "github.com/myorg/myhooks",
            },
        },
    }
}

// Hook implementations receive context about the function call
func BeforeProcessRequest(ctx *hooks.HookContext) error {
    fmt.Printf("[TRACE] Entering %s\n", ctx.Function)
    return nil
}

func AfterProcessRequest(ctx *hooks.HookContext) error {
    fmt.Printf("[TRACE] Exiting %s (duration: %v)\n", ctx.Function, ctx.Duration)
    return nil
}

func BeforeHandle(ctx *hooks.HookContext) error {
    fmt.Printf("[TRACE] Server.Handle called\n")
    return nil
}

func AfterHandle(ctx *hooks.HookContext) error {
    if ctx.Error != nil {
        fmt.Printf("[ERROR] Server.Handle failed: %v\n", ctx.Error)
    }
    return nil
}

var _ hooks.HookProvider = (*MyHookProvider)(nil)
```

### Hook Context

The `HookContext` struct provides information about the hooked function call:

```go
type HookContext struct {
    Package  string          // Target package name
    Function string          // Target function name
    Receiver string          // Receiver type (for methods)
    Args     []interface{}   // Function arguments
    StartTime time.Time      // When the function was called
    Result   interface{}     // Return value (After hooks only)
    Error    error           // Error return (After hooks only)
    Duration time.Duration   // Execution time (After hooks only)
    Ctx      context.Context // User context
}
```

### Function Rewriting

For more advanced use cases, you can completely rewrite a function's AST:

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
        // This allows changing signatures, adding parameters,
        // or completely replacing the function body

        return funcDecl, nil
    },
}
```

## Web UI

The project includes a web-based IDE (`ui/`) for interactive exploration and code editing with full Go language support.

### Features

- **Monaco Editor** - VS Code's editor with syntax highlighting, bracket matching, and minimap
- **Go IntelliSense** - Autocomplete, hover documentation, go-to-definition via gopls LSP
- **File Explorer** - Directory navigation with file tree
- **Tab Management** - Multiple open files with unsaved change indicators
- **Static Call Graph** - Visualization with multi-selection for hook generation
- **Hook Generation** - Auto-generate hook code from selected functions
- **Build Integration** - Compile and run with custom hooks

### Building the UI

**Prerequisites:**
- Go 1.18+
- Node.js and npm (for downloading Monaco Editor)
- gopls (auto-installed if missing)

**Quick Start (using Makefile):**

```bash
cd ui
make setup    # Full setup: install deps, copy Monaco, build
make run      # Run with ../hello project
```

**Available Make Targets:**

| Target | Description |
|--------|-------------|
| `make setup` | Full setup (deps + monaco + build) |
| `make build` | Build the UI server |
| `make run` | Build and run with ../hello |
| `make run-dir DIR=/path` | Run with custom project |
| `make clean` | Remove build artifacts |
| `make help` | Show all targets |

**Manual Setup:**

```bash
cd ui

# Install Monaco Editor (one-time setup)
npm install monaco-editor@0.45.0

# Copy Monaco to static directory
cp -r node_modules/monaco-editor/min/vs static/monaco/

# Build the UI server
go build -o ui .
```

**Running:**

```bash
# Start the UI server pointing to your Go project
./ui -dir /path/to/your/go/project

# Or use default port 9090
./ui -dir ../hello
```

Open http://localhost:9090 in your browser.

### LSP Features

When editing `.go` files, you get full language support from gopls:

- **Autocomplete** - Type `fmt.` to see all package methods
- **Hover** - Hover over symbols for documentation
- **Go to Definition** - Ctrl+Click or F12
- **Diagnostics** - Real-time error and warning display
- **Signature Help** - Parameter hints when typing function calls

## How It Works

### Build Interception Flow

1. **Capture Phase**: Run `go build -x -a -work -json` to capture all build commands
2. **Parse Phase**: Extract compile commands, identifying source files and packages
3. **Analysis Phase**: Parse Go source files to find function declarations
4. **Match Phase**: Compare functions against hook definitions
5. **Instrument Phase**: For matched functions, inject trampoline calls
6. **Replay Phase**: Execute modified build commands with instrumented sources

### Instrumentation Details

When a function matches a hook definition, the tool:

1. Creates a copy of the source file in the WORK directory
2. Parses the AST of the copied file
3. Injects a call to `trampoline_BeforeXXX()` at the function start
4. Wraps the function body with `defer trampoline_AfterXXX()` for cleanup
5. Adds trampoline function definitions that call the actual hooks
6. Updates the build commands to use the instrumented files

## Command Line Reference

| Flag | Description |
|------|-------------|
| `--log <file>` | Path to build log file (default: go-build.log) |
| `--capture` | Capture go build output to go-build.log |
| `--json` | Capture go build JSON output (recommended) |
| `--execute` | Execute the generated replay script |
| `--interactive` | Step through commands interactively |
| `--dry-run` | Show commands without executing |
| `--verbose` | Show detailed command information |
| `--dump` | Dump raw parsed commands |
| `--pack-files` | List files from compile commands |
| `--pack-functions` | Extract function definitions |
| `--pack-packages` | List package names |
| `--pack-packagepath` | Show packages with source paths |
| `--callgraph` | Generate static call graph |
| `--workdir` | Inspect WORK directory contents |
| `--compile <file>` | Compile with hook instrumentation |
| `-c <file>` | Short form of --compile |

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
│           └── vs/      # Monaco loader and editor
└── examples/            # Example applications
    ├── hello/
    │   ├── main.go
    │   └── hello_hook/
    │       └── hello_hooks.go  # Example hook definitions
    └── simple-http-server/
```

## Generated Files

During operation, the tool creates several files:

| File | Description |
|------|-------------|
| `go-build.log` | Captured build commands (text format) |
| `go-build.json` | Raw JSON build output (when using --json) |
| `go-build-modified.log` | Build log with paths updated for instrumented files |
| `replay_script.sh` | Executable bash script to replay the build |

## Requirements

- Go 1.18 or later
- Unix-like operating system (Linux, macOS)
- `golang.org/x/tools/go/packages` for package analysis

## Limitations

- Currently supports before/after style hooks; more complex interception patterns require function rewriting
- Static call graph analysis may not capture all dynamic dispatch scenarios
- Some edge cases in Go's build system may not be fully captured

## Contributing

Contributions are welcome. Please ensure that:

1. New features include appropriate tests
2. Code follows Go conventions and passes `go vet` and `golint`
3. Documentation is updated for new functionality

## License

This project is provided under the MIT License. See LICENSE file for details.