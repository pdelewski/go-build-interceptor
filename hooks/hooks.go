package hooks

import (
	"context"
	"fmt"
	"time"
)

// Core hook definition
type Hook struct {
	Target InjectTarget
	Hooks  InjectFunctions
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

// Validation
func (h *Hook) Validate() error {
	if h.Target.Package == "" {
		return fmt.Errorf("target package is required")
	}
	if h.Target.Function == "" {
		return fmt.Errorf("target function is required")
	}
	if h.Target.Receiver == "" {
		return fmt.Errorf("target receiver is required")
	}
	if h.Hooks.Before == "" {
		return fmt.Errorf("before hook function name is required")
	}
	if h.Hooks.After == "" {
		return fmt.Errorf("after hook function name is required")
	}
	if h.Hooks.From == "" {
		return fmt.Errorf("hook package path is required")
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