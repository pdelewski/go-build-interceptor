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
				Function: "foo",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeFoo",
				After:  "AfterFoo",
				From:   "generated_hooks",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar1",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeBar1",
				After:  "AfterBar1",
				From:   "generated_hooks",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar2",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeBar2",
				After:  "AfterBar2",
				From:   "generated_hooks",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "main",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeMain",
				After:  "AfterMain",
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

// BeforeFoo is called before foo() executes
// The HookContext allows passing data to the After hook and skipping the original call
func BeforeFoo(ctx hooks.HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// AfterFoo is called after foo() completes
func AfterFoo(ctx hooks.HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}

// BeforeBar1 is called before bar1() executes
// The HookContext allows passing data to the After hook and skipping the original call
func BeforeBar1(ctx hooks.HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// AfterBar1 is called after bar1() completes
func AfterBar1(ctx hooks.HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}

// BeforeBar2 is called before bar2() executes
// The HookContext allows passing data to the After hook and skipping the original call
func BeforeBar2(ctx hooks.HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// AfterBar2 is called after bar2() completes
func AfterBar2(ctx hooks.HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}

// BeforeMain is called before main() executes
// The HookContext allows passing data to the After hook and skipping the original call
func BeforeMain(ctx hooks.HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// AfterMain is called after main() completes
func AfterMain(ctx hooks.HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}
