package hello_hook

import (
	"fmt"
	"strings"
	"sync"
	"github.com/pdelewski/go-build-interceptor/hooks"
)

// CallTracer provides advanced call tracing capabilities
type CallTracer struct {
	mu       sync.Mutex
	depth    int
	calls    []string
}

// NewCallTracer creates a new call tracer
func NewCallTracer() *CallTracer {
	return &CallTracer{
		calls: make([]string, 0),
	}
}

// GetIndent returns the indentation string based on call depth
func (ct *CallTracer) GetIndent() string {
	return strings.Repeat("  ", ct.depth)
}

// TracingHookProvider provides hooks with call tracing
type TracingHookProvider struct {
	tracer *CallTracer
}

// NewTracingHookProvider creates a new tracing hook provider
func NewTracingHookProvider() *TracingHookProvider {
	return &TracingHookProvider{
		tracer: NewCallTracer(),
	}
}

// ProvideHooks returns hooks with tracing capabilities
func (t *TracingHookProvider) ProvideHooks() []*hooks.Hook {
	return []*hooks.Hook{
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "main",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "TracingBeforeMain",
				After:  "TracingAfterMain",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "foo",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "TracingBeforeFoo",
				After:  "TracingAfterFoo",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar1",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "TracingBeforeBar1",
				After:  "TracingAfterBar1",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar2",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "TracingBeforeBar2",
				After:  "TracingAfterBar2",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
	}
}

// Global tracer instance for the tracing hooks
var globalTracer = NewCallTracer()

// Tracing hook implementations
func TracingBeforeMain(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	fmt.Printf("%s→ main()\n", globalTracer.GetIndent())
	globalTracer.depth++
	globalTracer.calls = append(globalTracer.calls, "main")
	return nil
}

func TracingAfterMain(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	globalTracer.depth--
	fmt.Printf("%s← main() [%v]\n", globalTracer.GetIndent(), ctx.Duration)
	return nil
}

func TracingBeforeFoo(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	fmt.Printf("%s→ foo()\n", globalTracer.GetIndent())
	globalTracer.depth++
	globalTracer.calls = append(globalTracer.calls, "foo")
	return nil
}

func TracingAfterFoo(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	globalTracer.depth--
	fmt.Printf("%s← foo() [%v]\n", globalTracer.GetIndent(), ctx.Duration)
	return nil
}

func TracingBeforeBar1(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	fmt.Printf("%s→ bar1()\n", globalTracer.GetIndent())
	globalTracer.depth++
	globalTracer.calls = append(globalTracer.calls, "bar1")
	return nil
}

func TracingAfterBar1(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	globalTracer.depth--
	fmt.Printf("%s← bar1() [%v]\n", globalTracer.GetIndent(), ctx.Duration)
	return nil
}

func TracingBeforeBar2(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	fmt.Printf("%s→ bar2()\n", globalTracer.GetIndent())
	globalTracer.depth++
	globalTracer.calls = append(globalTracer.calls, "bar2")
	return nil
}

func TracingAfterBar2(ctx *hooks.RuntimeHookContext) error {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	globalTracer.depth--
	fmt.Printf("%s← bar2() [%v]\n", globalTracer.GetIndent(), ctx.Duration)
	return nil
}

// GetCallTrace returns the recorded call trace
func GetCallTrace() []string {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	result := make([]string, len(globalTracer.calls))
	copy(result, globalTracer.calls)
	return result
}

// ResetCallTrace resets the call trace
func ResetCallTrace() {
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	
	globalTracer.depth = 0
	globalTracer.calls = globalTracer.calls[:0]
}