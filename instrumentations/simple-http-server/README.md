# Simple HTTP Server Instrumentation

Hook definitions for tracing HTTP handlers in the simple-http-server example.

## What it does

Provides before/after hooks for HTTP handlers:
- `homeHandler()` - Home page handler
- `helloHandler()` - Greeting endpoint handler

Each hook:
- Records the start time before handler execution
- Logs handler entry with package and function name
- Logs handler exit with execution duration

## Usage

```bash
cd examples/simple-http-server
../../hc/hc -c ../../instrumentations/simple-http-server/simple-http-hooks.go
./simple-http-server
```

Then visit http://localhost:8080 and watch the console output.

## Example Output

```
Starting HTTP server on http://localhost:8080
[BEFORE] main.homeHandler()
[AFTER] main.homeHandler() completed in 45.291µs
[BEFORE] main.helloHandler()
[AFTER] main.helloHandler() completed in 12.125µs
```

## Files

- `simple-http-hooks.go` - Hook definitions and implementations