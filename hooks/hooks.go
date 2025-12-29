package hooks

import (
	"context"
	"fmt"
	"go/ast"
	"time"
)

// FunctionRewriteHook allows complete rewriting of a function's AST.
// Use this type when assigning to Hook.Rewrite field.
type FunctionRewriteHook func(originalNode ast.Node) (ast.Node, error)

// RuntimeHookContext provides a full-featured context for hook functions.
// This is used by advanced hooks that need access to timing, results, and context.
type RuntimeHookContext struct {
	// Target information
	Package  string
	Function string
	Receiver string

	// Runtime data
	Args      []interface{}
	StartTime time.Time

	// For After hooks only
	Result   interface{}
	Error    error
	Duration time.Duration

	// User context
	Ctx context.Context
}

// Function signature stubs for advanced hook implementations
type BeforeHook func(hookCtx *RuntimeHookContext) error
type AfterHook func(hookCtx *RuntimeHookContext) error

// HookProvider interface that users must implement to provide their hooks
type HookProvider interface {
	ProvideHooks() []*Hook
}

// Validation
func (h *Hook) Validate() error {
	if h.Target.Package == "" {
		return fmt.Errorf("target package is required")
	}
	if h.Target.Function == "" {
		return fmt.Errorf("target function is required")
	}
	// Receiver can be empty for package-level functions

	// Must have either Hooks or Rewrite specified
	if h.Hooks == nil && h.Rewrite == nil {
		return fmt.Errorf("either Hooks or Rewrite must be specified")
	}

	// If Hooks is specified, validate it
	if h.Hooks != nil {
		if h.Hooks.Before == "" && h.Hooks.After == "" {
			return fmt.Errorf("at least one of Before or After hook must be specified")
		}
		if h.Hooks.From == "" {
			return fmt.Errorf("hook package path is required when using Hooks")
		}
	}

	return nil
}

// Registry for managing multiple hooks
type Registry struct {
	hooks []*Hook
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Add(hook *Hook) error {
	if err := hook.Validate(); err != nil {
		return err
	}
	r.hooks = append(r.hooks, hook)
	return nil
}

func (r *Registry) MustAdd(hook *Hook) *Registry {
	if err := r.Add(hook); err != nil {
		panic(err)
	}
	return r
}

func (r *Registry) GetHooks() []*Hook {
	return r.hooks
}
