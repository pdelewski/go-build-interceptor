package hooks

import (
	"context"
	"fmt"
	"go/ast"
	"time"
)

// FunctionRewriteHook allows complete rewriting of a function's AST
type FunctionRewriteHook func(originalNode ast.Node) (ast.Node, error)

// Core hook definition
type Hook struct {
	Target  InjectTarget
	Hooks   *InjectFunctions     // Optional: for before/after hooks
	Rewrite FunctionRewriteHook  // Optional: for rewriting entire function
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

// Framework-provided context for hook functions
type HookContext struct {
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

// Function signature stubs that all hook implementations must follow
type BeforeHook func(hookCtx *HookContext) error
type AfterHook func(hookCtx *HookContext) error

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
	
	// Must have either Hooks or Rewrite, but not necessarily both
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