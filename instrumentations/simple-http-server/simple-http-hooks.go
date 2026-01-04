package generated_hooks

import (
	"fmt"
	"time"
	_ "unsafe" // Required for go:linkname

	"github.com/pdelewski/go-build-interceptor/hooks"
)

// ============================================================================
// Hook Provider (for go-build-interceptor parsing)
// ============================================================================

// ProvideHooks returns the hook definitions for the selected functions
func ProvideHooks() []*hooks.Hook {
	return []*hooks.Hook{
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "homeHandler",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeHomeHandler",
				After:  "AfterHomeHandler",
				From:   "generated_hooks",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "helloHandler",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeHelloHandler",
				After:  "AfterHelloHandler",
				From:   "generated_hooks",
			},
		},
	}
}

// ============================================================================
// Hook Implementations
// ============================================================================
// These functions are called via go:linkname from the instrumented code.
// The instrumented code generates trampoline functions that link to these.

// BeforeHomeHandler is called before homeHandler() executes
// The HookContext allows passing data to the After hook and skipping the original call
func BeforeHomeHandler(ctx hooks.HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// AfterHomeHandler is called after homeHandler() completes
func AfterHomeHandler(ctx hooks.HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}

// BeforeHelloHandler is called before helloHandler() executes
// The HookContext allows passing data to the After hook and skipping the original call
func BeforeHelloHandler(ctx hooks.HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// AfterHelloHandler is called after helloHandler() completes
func AfterHelloHandler(ctx hooks.HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}
