// Package hooks provides hook type definitions for go-build-interceptor.
// This file contains lightweight types with no external dependencies.
package hooks

// Hook defines a hook with its target function and hook implementations
type Hook struct {
	Target InjectTarget
	Hooks  *InjectFunctions // Optional: for before/after hooks
}

// InjectTarget specifies the target function to instrument
type InjectTarget struct {
	Package  string
	Function string
	Receiver string
}

// InjectFunctions specifies the before/after hook functions
type InjectFunctions struct {
	Before string
	After  string
	From   string
}
