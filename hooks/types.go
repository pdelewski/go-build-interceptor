// Package hooks provides hook type definitions for go-build-interceptor.
// This file contains lightweight types with no external dependencies.
package hooks

// Hook defines a hook with its target function and hook implementations
type Hook struct {
	Target  InjectTarget
	Hooks   *InjectFunctions // Optional: for before/after hooks
	Rewrite interface{}      // Optional: FunctionRewriteHook for rewriting entire function
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

// HookContext provides a minimal interface for hook functions.
// This interface is implemented by the generated trampoline code.
type HookContext interface {
	SetData(data interface{})
	GetData() interface{}
	SetKeyData(key string, val interface{})
	GetKeyData(key string) interface{}
	HasKeyData(key string) bool
	SetSkipCall(skip bool)
	IsSkipCall() bool
	GetFuncName() string
	GetPackageName() string
}

// StructField defines a field to be added to a struct
type StructField struct {
	Name string // Field name
	Type string // Field type
}

// StructModification defines a modification to a struct in a package
type StructModification struct {
	Package    string        // Target package
	StructName string        // Name of the struct to modify
	AddFields  []StructField // Fields to add
}

// GeneratedFile defines a file to be generated into a package
type GeneratedFile struct {
	Package  string // Target package
	FileName string // Name of the file to generate
	Content  string // The Go source code content
}
