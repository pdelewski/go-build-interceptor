package hooks

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// Mock ResponseWriter for testing
type mockResponseWriter struct{}

func (m *mockResponseWriter) Header() http.Header        { return make(http.Header) }
func (m *mockResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (m *mockResponseWriter) WriteHeader(statusCode int) {}

// Example implementation of HTTP server hooks
func BeforeServeHTTP(ctx *HookContext) error {
	// Type assert the arguments
	_, ok := ctx.Args[0].(http.ResponseWriter)
	if !ok {
		return fmt.Errorf("expected ResponseWriter, got %T", ctx.Args[0])
	}

	req, ok := ctx.Args[1].(*http.Request)
	if !ok {
		return fmt.Errorf("expected *Request, got %T", ctx.Args[1])
	}

	// Your instrumentation logic
	fmt.Printf("[%s.%s] Starting HTTP request: %s %s\n",
		ctx.Receiver, ctx.Function, req.Method, req.URL.Path)

	return nil
}

func AfterServeHTTP(ctx *HookContext) error {
	req := ctx.Args[1].(*http.Request)

	fmt.Printf("[%s.%s] Completed HTTP request in %v: %s %s\n",
		ctx.Receiver, ctx.Function, ctx.Duration, req.Method, req.URL.Path)

	if ctx.Error != nil {
		fmt.Printf("HTTP request failed: %v\n", ctx.Error)
	}

	return nil
}

// Example implementation of SQL hooks
func BeforeQuery(ctx *HookContext) error {
	query, ok := ctx.Args[0].(string)
	if !ok {
		return fmt.Errorf("expected string query, got %T", ctx.Args[0])
	}

	fmt.Printf("[%s.%s] Executing SQL query: %s\n",
		ctx.Receiver, ctx.Function, query)

	return nil
}

func AfterQuery(ctx *HookContext) error {
	query := ctx.Args[0].(string)

	fmt.Printf("[%s.%s] SQL query completed in %v: %s\n",
		ctx.Receiver, ctx.Function, ctx.Duration, query)

	if ctx.Error != nil {
		fmt.Printf("SQL query failed: %v\n", ctx.Error)
	}

	return nil
}

func TestHookFramework(t *testing.T) {
	// Create registry with multiple hooks
	registry := NewRegistry().
		MustAdd(&Hook{
			Target: InjectTarget{
				Package:  "net/http",
				Function: "ServeHTTP",
				Receiver: "serverHandler",
			},
			Hooks: InjectFunctions{
				Before: "BeforeServeHTTP",
				After:  "AfterServeHTTP",
				From:   "github.com/yourorg/instrumentation/nethttp/server",
			},
		}).
		MustAdd(&Hook{
			Target: InjectTarget{
				Package:  "database/sql",
				Function: "Query",
				Receiver: "DB",
			},
			Hooks: InjectFunctions{
				Before: "BeforeQuery",
				After:  "AfterQuery",
				From:   "github.com/yourorg/instrumentation/sql",
			},
		})

	// Verify hooks were registered
	registeredHooks := registry.GetHooks()
	if len(registeredHooks) != 2 {
		t.Errorf("Expected 2 hooks, got %d", len(registeredHooks))
	}

	// Test hook validation
	for _, hook := range registeredHooks {
		if err := hook.Validate(); err != nil {
			t.Errorf("Hook validation failed: %v", err)
		}
	}
}

func TestManualHookCreation(t *testing.T) {
	// Test manual HTTP Server hook creation
	httpHook := &Hook{
		Target: InjectTarget{
			Package:  "net/http",
			Function: "ServeHTTP",
			Receiver: "serverHandler",
		},
		Hooks: InjectFunctions{
			Before: "BeforeServeHTTP",
			After:  "AfterServeHTTP",
			From:   "github.com/yourorg/instrumentation/nethttp/server",
		},
	}

	if err := httpHook.Validate(); err != nil {
		t.Errorf("HTTP server hook validation failed: %v", err)
	}

	if httpHook.Target.Package != "net/http" {
		t.Errorf("Expected package 'net/http', got '%s'", httpHook.Target.Package)
	}

	// Test manual SQL hook creation
	sqlHook := &Hook{
		Target: InjectTarget{
			Package:  "database/sql",
			Function: "Query",
			Receiver: "DB",
		},
		Hooks: InjectFunctions{
			Before: "BeforeQuery",
			After:  "AfterQuery",
			From:   "github.com/yourorg/instrumentation/sql",
		},
	}

	if err := sqlHook.Validate(); err != nil {
		t.Errorf("SQL hook validation failed: %v", err)
	}

	// Test manual custom hook creation
	customHook := &Hook{
		Target: InjectTarget{
			Package:  "github.com/myapp/orders",
			Function: "ProcessOrder",
			Receiver: "OrderService",
		},
		Hooks: InjectFunctions{
			Before: "BeforeProcessOrder",
			After:  "AfterProcessOrder",
			From:   "github.com/myorg/instrumentation/orders",
		},
	}

	if err := customHook.Validate(); err != nil {
		t.Errorf("Custom hook validation failed: %v", err)
	}
}

func TestHookContext(t *testing.T) {
	// Create a mock HTTP request
	req := &http.Request{
		Method: "GET",
		URL:    &url.URL{Path: "/test"},
	}

	// Simulate a hook context for HTTP request
	ctx := &HookContext{
		Package:   "net/http",
		Function:  "ServeHTTP",
		Receiver:  "serverHandler",
		Args:      []interface{}{&mockResponseWriter{}, req},
		StartTime: time.Now(),
		Ctx:       context.Background(),
	}

	// Test Before hook
	if err := BeforeServeHTTP(ctx); err != nil {
		t.Errorf("BeforeServeHTTP failed: %v", err)
	}

	// Simulate completion
	ctx.Duration = time.Since(ctx.StartTime)

	// Test After hook
	if err := AfterServeHTTP(ctx); err != nil {
		t.Errorf("AfterServeHTTP failed: %v", err)
	}
}

func ExampleRegistry() {
	// Create a registry and add multiple hooks
	registry := NewRegistry().
		MustAdd(&Hook{
			Target: InjectTarget{
				Package:  "net/http",
				Function: "ServeHTTP",
				Receiver: "serverHandler",
			},
			Hooks: InjectFunctions{
				Before: "BeforeServeHTTP",
				After:  "AfterServeHTTP",
				From:   "github.com/yourorg/instrumentation/nethttp/server",
			},
		}).
		MustAdd(&Hook{
			Target: InjectTarget{
				Package:  "database/sql",
				Function: "Query",
				Receiver: "DB",
			},
			Hooks: InjectFunctions{
				Before: "BeforeQuery",
				After:  "AfterQuery",
				From:   "github.com/yourorg/instrumentation/sql",
			},
		}).
		MustAdd(&Hook{
			Target: InjectTarget{
				Package:  "github.com/myapp/business",
				Function: "ProcessPayment",
				Receiver: "PaymentService",
			},
			Hooks: InjectFunctions{
				Before: "BeforeProcessPayment",
				After:  "AfterProcessPayment",
				From:   "github.com/myorg/instrumentation/payment",
			},
		})

	// Get all hooks for processing
	hooks := registry.GetHooks()
	fmt.Printf("Registered %d hooks\n", len(hooks))

	// Output: Registered 3 hooks
}
