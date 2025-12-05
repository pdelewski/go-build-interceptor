package hello_hook

import (
	"fmt"
	"github.com/pdelewski/go-build-interceptor/hooks"
)

// HelloHookProvider implements the HookProvider interface for the hello module
type HelloHookProvider struct{}

// ProvideHooks returns the hook definitions for the hello module functions
func (h *HelloHookProvider) ProvideHooks() []*hooks.Hook {
	return []*hooks.Hook{
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "foo",
				Receiver: "",
			},
			Hooks: hooks.InjectFunctions{
				Before: "BeforeFoo",
				After:  "AfterFoo",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar1",
				Receiver: "",
			},
			Hooks: hooks.InjectFunctions{
				Before: "BeforeBar1",
				After:  "AfterBar1",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar2",
				Receiver: "",
			},
			Hooks: hooks.InjectFunctions{
				Before: "BeforeBar2",
				After:  "AfterBar2",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "main",
				Receiver: "",
			},
			Hooks: hooks.InjectFunctions{
				Before: "BeforeMain",
				After:  "AfterMain",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
	}
}

// Hook implementations for foo function
func BeforeFoo(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Starting foo()\n", ctx.Function)
	return nil
}

func AfterFoo(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Completed foo() in %v\n", ctx.Function, ctx.Duration)
	return nil
}

// Hook implementations for bar1 function
func BeforeBar1(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Starting bar1()\n", ctx.Function)
	return nil
}

func AfterBar1(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Completed bar1() in %v\n", ctx.Function, ctx.Duration)
	return nil
}

// Hook implementations for bar2 function
func BeforeBar2(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Starting bar2()\n", ctx.Function)
	return nil
}

func AfterBar2(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Completed bar2() in %v\n", ctx.Function, ctx.Duration)
	return nil
}

// Hook implementations for main function
func BeforeMain(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Starting main()\n", ctx.Function)
	return nil
}

func AfterMain(ctx *hooks.HookContext) error {
	fmt.Printf("[%s] Completed main() in %v\n", ctx.Function, ctx.Duration)
	if ctx.Error != nil {
		fmt.Printf("Main function failed: %v\n", ctx.Error)
	}
	return nil
}

// Ensure HelloHookProvider implements the HookProvider interface
var _ hooks.HookProvider = (*HelloHookProvider)(nil)