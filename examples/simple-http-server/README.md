# Simple HTTP Server Example

A basic HTTP server demonstrating instrumentation of web handlers.

## What it does

This example provides a simple web server with three endpoints:
- `/` - Home page with navigation
- `/hello` - Greeting endpoint (accepts `?name=` parameter)
- `/time` - Current server time

## Building with instrumentation

```bash
# From this directory
../../hc/hc -c ../../instrumentations/simple-http-server/simple-http-hooks.go

# Run the instrumented server
./simple-http-server
```

## Testing

1. Start the server (runs on port 8080)
2. Open http://localhost:8080 in your browser
3. Navigate between pages and watch the console for instrumentation output

## Expected output

When you visit endpoints, you'll see tracing output:

```
Starting HTTP server on http://localhost:8080
[BEFORE] main.homeHandler()
[AFTER] main.homeHandler() completed in 45.291µs
[BEFORE] main.helloHandler()
[AFTER] main.helloHandler() completed in 12.125µs
```

## Instrumented functions

The hooks trace these handlers:
- `homeHandler` - Home page handler
- `helloHandler` - Greeting handler