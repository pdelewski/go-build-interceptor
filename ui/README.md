# UI - Web Interface

The web UI provides an interactive environment for exploring code, generating hooks, and building instrumented Go applications.

## What it does

- **Code Editor**: Monaco editor with Go syntax highlighting and LSP support
- **Static Call Graph**: Visualize function call relationships
- **Hook Generation**: Select functions and auto-generate hook boilerplate
- **Build & Run**: Compile with hooks and run directly from the browser
- **Debugging**: Step through instrumented code with breakpoints

## Files

| File | Description |
|------|-------------|
| `web_main.go` | Web server with HTTP handlers and LSP proxy |
| `static/` | Frontend assets (Monaco editor, CSS, JavaScript) |
| `Makefile` | Build automation for Linux/macOS |
| `build.bat` | Build automation for Windows |

## Setup

**Linux/macOS:**
```bash
make setup    # Install dependencies & build
make run      # Run with default project
```

**Windows:**
```cmd
build.bat setup    # Install dependencies & build
build.bat run      # Run with default project
```

**Manual setup:**
```bash
npm install monaco-editor@0.45.0
cp -r node_modules/monaco-editor/min/vs static/monaco/
go build -o ui .
./ui -dir /path/to/your/project
```

## Usage

1. Start the UI server: `./ui -dir /path/to/project`
2. Open http://localhost:9090 in your browser
3. Use View menu to explore functions, packages, and call graphs
4. Select functions and click "Generate Hooks" to create hook code
5. Use Run menu to compile and execute instrumented binaries

See the main [README](../README.md) for full documentation.