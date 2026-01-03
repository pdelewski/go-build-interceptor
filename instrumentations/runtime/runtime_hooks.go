package runtime_instrumentation

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/pdelewski/go-build-interceptor/hooks"
)

// RuntimeHookProvider provides hooks for runtime package instrumentation
// to enable context propagation across goroutines (GLS - Goroutine Local Storage)
type RuntimeHookProvider struct{}

// GetStructModifications returns the struct modifications needed for GLS
// This corresponds to add_gls_field in runtime.yaml
func (r *RuntimeHookProvider) GetStructModifications() []hooks.StructModification {
	return []hooks.StructModification{
		{
			Package:    "runtime",
			StructName: "g",
			AddFields: []hooks.StructField{
				{Name: "otel_trace_context", Type: "interface{}"},
				{Name: "otel_baggage_container", Type: "interface{}"},
			},
		},
	}
}

// GetGeneratedFiles returns files to be generated for GLS support
// This corresponds to gls_linker in runtime.yaml
func (r *RuntimeHookProvider) GetGeneratedFiles() []hooks.GeneratedFile {
	return []hooks.GeneratedFile{
		{
			Package:  "runtime",
			FileName: "runtime_gls.go",
			Content:  RuntimeGLSContent,
		},
	}
}

// ProvideHooks returns the hook definitions for raw code injection
// This corresponds to goroutine_propagate in runtime.yaml
func (r *RuntimeHookProvider) ProvideHooks() []*hooks.Hook {
	return []*hooks.Hook{
		{
			Target: hooks.InjectTarget{
				Package:  "runtime",
				Function: "newproc1",
				Receiver: "",
			},
			Rewrite: RewriteNewproc1,
		},
	}
}

// RewriteNewproc1 injects context propagation code into newproc1
// This implements the goroutine_propagate rule from runtime.yaml
func RewriteNewproc1(originalNode ast.Node) (ast.Node, error) {
	funcDecl, ok := originalNode.(*ast.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("expected *ast.FuncDecl, got %T", originalNode)
	}

	// Rename unnamed return values so raw code can reference them
	renameReturnValues(funcDecl)

	// Parse the raw code to inject
	rawCode := `defer func(){
		_unnamedRetVal0.otel_trace_context = propagateOtelContext(callergp.otel_trace_context)
		_unnamedRetVal0.otel_baggage_container = propagateOtelContext(callergp.otel_baggage_container)
	}()`

	stmts, err := parseSnippet(rawCode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse raw code: %w", err)
	}

	// Insert at the beginning of the function body
	funcDecl.Body.List = append(stmts, funcDecl.Body.List...)

	return funcDecl, nil
}

// renameReturnValues renames unnamed return values to _unnamedRetVal0, _unnamedRetVal1, etc.
func renameReturnValues(funcDecl *ast.FuncDecl) {
	if funcDecl.Type.Results == nil {
		return
	}
	idx := 0
	for _, field := range funcDecl.Type.Results.List {
		if field.Names == nil {
			name := fmt.Sprintf("_unnamedRetVal%d", idx)
			field.Names = []*ast.Ident{ast.NewIdent(name)}
			idx++
		}
	}
}

// parseSnippet parses a code snippet into AST statements
func parseSnippet(code string) ([]ast.Stmt, error) {
	// Wrap in a function to make it parseable
	wrapped := fmt.Sprintf("package p\nfunc f() {\n%s\n}", code)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", wrapped, 0)
	if err != nil {
		return nil, err
	}

	// Extract statements from the function body
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn.Body.List, nil
		}
	}
	return nil, fmt.Errorf("no function found in parsed snippet")
}

// RuntimeGLSContent is the content of runtime_gls.go
// This provides accessor functions for goroutine-local storage
const RuntimeGLSContent = `package runtime

func GetTraceContextFromGLS() interface{} {
	return getg().m.curg.otel_trace_context
}

func GetBaggageContainerFromGLS() interface{} {
	return getg().m.curg.otel_baggage_container
}

func SetTraceContextToGLS(traceContext interface{}) {
	getg().m.curg.otel_trace_context = traceContext
}

func SetBaggageContainerToGLS(baggageContainer interface{}) {
	getg().m.curg.otel_baggage_container = baggageContainer
}

type OtelContextCloner interface {
	Clone() interface{}
}

func propagateOtelContext(context interface{}) interface{} {
	if context == nil {
		return nil
	}
	if cloner, ok := context.(OtelContextCloner); ok {
		return cloner.Clone()
	}
	return context
}
`

// Ensure RuntimeHookProvider implements the HookProvider interface
var _ hooks.HookProvider = (*RuntimeHookProvider)(nil)