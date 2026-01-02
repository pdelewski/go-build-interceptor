package runtime_instrumentation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestGetStructModifications(t *testing.T) {
	provider := &RuntimeHookProvider{}
	mods := provider.GetStructModifications()

	if len(mods) != 1 {
		t.Fatalf("expected 1 struct modification, got %d", len(mods))
	}

	mod := mods[0]
	if mod.Package != "runtime" {
		t.Errorf("expected package 'runtime', got '%s'", mod.Package)
	}
	if mod.StructName != "g" {
		t.Errorf("expected struct 'g', got '%s'", mod.StructName)
	}
	if len(mod.AddFields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(mod.AddFields))
	}

	expectedFields := map[string]string{
		"otel_trace_context":     "interface{}",
		"otel_baggage_container": "interface{}",
	}
	for _, field := range mod.AddFields {
		expectedType, ok := expectedFields[field.Name]
		if !ok {
			t.Errorf("unexpected field: %s", field.Name)
			continue
		}
		if field.Type != expectedType {
			t.Errorf("field %s: expected type '%s', got '%s'", field.Name, expectedType, field.Type)
		}
	}
}

func TestGetGeneratedFiles(t *testing.T) {
	provider := &RuntimeHookProvider{}
	files := provider.GetGeneratedFiles()

	if len(files) != 1 {
		t.Fatalf("expected 1 generated file, got %d", len(files))
	}

	file := files[0]
	if file.Package != "runtime" {
		t.Errorf("expected package 'runtime', got '%s'", file.Package)
	}
	if file.FileName != "runtime_gls.go" {
		t.Errorf("expected filename 'runtime_gls.go', got '%s'", file.FileName)
	}
	if file.Content == "" {
		t.Error("expected non-empty content")
	}

	// Verify content is valid Go code
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "runtime_gls.go", file.Content, 0)
	if err != nil {
		t.Errorf("generated content is not valid Go: %v", err)
	}
}

func TestProvideHooks(t *testing.T) {
	provider := &RuntimeHookProvider{}
	hooks := provider.ProvideHooks()

	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}

	hook := hooks[0]
	if hook.Target.Package != "runtime" {
		t.Errorf("expected package 'runtime', got '%s'", hook.Target.Package)
	}
	if hook.Target.Function != "newproc1" {
		t.Errorf("expected function 'newproc1', got '%s'", hook.Target.Function)
	}
	if hook.Rewrite == nil {
		t.Error("expected Rewrite to be set")
	}
}

func TestRewriteNewproc1(t *testing.T) {
	// Create a mock newproc1 function
	src := `package runtime
func newproc1(fn *funcval, callergp *g, callerpc uintptr) *g {
	// original function body
	return nil
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("failed to parse test source: %v", err)
	}

	// Find the function declaration
	var funcDecl *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "newproc1" {
			funcDecl = fn
			break
		}
	}

	if funcDecl == nil {
		t.Fatal("failed to find newproc1 function")
	}

	// Apply the rewrite
	result, err := RewriteNewproc1(funcDecl)
	if err != nil {
		t.Fatalf("RewriteNewproc1 failed: %v", err)
	}

	rewrittenFunc, ok := result.(*ast.FuncDecl)
	if !ok {
		t.Fatalf("expected *ast.FuncDecl, got %T", result)
	}

	// Verify the return value was renamed
	if rewrittenFunc.Type.Results == nil || len(rewrittenFunc.Type.Results.List) == 0 {
		t.Fatal("expected return value")
	}

	retField := rewrittenFunc.Type.Results.List[0]
	if len(retField.Names) == 0 || retField.Names[0].Name != "_unnamedRetVal0" {
		t.Error("expected return value to be renamed to _unnamedRetVal0")
	}

	// Verify code was injected at the beginning
	if len(rewrittenFunc.Body.List) < 2 {
		t.Error("expected injected code in function body")
	}

	// First statement should be a defer
	firstStmt := rewrittenFunc.Body.List[0]
	if _, ok := firstStmt.(*ast.DeferStmt); !ok {
		t.Errorf("expected first statement to be defer, got %T", firstStmt)
	}
}

func TestParseSnippet(t *testing.T) {
	code := `x := 1
y := 2`

	stmts, err := parseSnippet(code)
	if err != nil {
		t.Fatalf("parseSnippet failed: %v", err)
	}

	if len(stmts) != 2 {
		t.Errorf("expected 2 statements, got %d", len(stmts))
	}
}