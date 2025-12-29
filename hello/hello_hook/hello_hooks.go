package hello_hook

import (
	"fmt"
	"go/ast"
	"go/token"
	"github.com/pdelewski/go-build-interceptor/hooks"
)

// HelloHookProvider implements the HookProvider interface for the hello module
type HelloHookProvider struct{}

// ProvideHooks returns the hook definitions for the hello module functions
func (h *HelloHookProvider) ProvideHooks() []*hooks.Hook {
	return []*hooks.Hook{
		// Traditional before/after hooks
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "foo",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeFoo",
				After:  "AfterFoo",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
		// Example of function rewrite
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar1",
				Receiver: "",
			},
			Rewrite: RewriteBar1,
		},
		// Combination: both hooks and rewrite
		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "bar2",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "BeforeBar2",
				After:  "AfterBar2",
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
			Rewrite: RewriteBar2,
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
				From:   "github.com/pdelewski/go-build-interceptor/hello_hook",
			},
		},
	}
}

// Hook implementations for foo function
func BeforeFoo(ctx *hooks.RuntimeHookContext) error {
	fmt.Printf("[%s] Starting foo()\n", ctx.Function)
	return nil
}

func AfterFoo(ctx *hooks.RuntimeHookContext) error {
	fmt.Printf("[%s] Completed foo() in %v\n", ctx.Function, ctx.Duration)
	return nil
}

// RewriteBar1 demonstrates complete function rewriting
func RewriteBar1(originalNode ast.Node) (ast.Node, error) {
	funcDecl, ok := originalNode.(*ast.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("expected *ast.FuncDecl, got %T", originalNode)
	}
	
	// Change function signature: add a string parameter and string return type
	funcDecl.Type.Params = &ast.FieldList{
		List: []*ast.Field{
			{
				Names: []*ast.Ident{ast.NewIdent("name")},
				Type:  ast.NewIdent("string"),
			},
		},
	}
	
	funcDecl.Type.Results = &ast.FieldList{
		List: []*ast.Field{
			{
				Type: ast.NewIdent("string"),
			},
		},
	}
	
	// Create new function body
	funcDecl.Body = &ast.BlockStmt{
		List: []ast.Stmt{
			// return fmt.Sprintf("Hello from rewritten bar1, %s!", name)
			&ast.ReturnStmt{
				Results: []ast.Expr{
					&ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   ast.NewIdent("fmt"),
							Sel: ast.NewIdent("Sprintf"),
						},
						Args: []ast.Expr{
							&ast.BasicLit{
								Kind:  token.STRING,
								Value: `"Hello from rewritten bar1, %s!"`,
							},
							ast.NewIdent("name"),
						},
					},
				},
			},
		},
	}
	
	return funcDecl, nil
}

// RewriteBar2 demonstrates rewriting while still allowing hooks
func RewriteBar2(originalNode ast.Node) (ast.Node, error) {
	funcDecl, ok := originalNode.(*ast.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("expected *ast.FuncDecl, got %T", originalNode)
	}
	
	// Add an int parameter
	funcDecl.Type.Params = &ast.FieldList{
		List: []*ast.Field{
			{
				Names: []*ast.Ident{ast.NewIdent("count")},
				Type:  ast.NewIdent("int"),
			},
		},
	}
	
	// Simple body that uses the parameter
	funcDecl.Body = &ast.BlockStmt{
		List: []ast.Stmt{
			// for i := 0; i < count; i++
			&ast.ForStmt{
				Init: &ast.AssignStmt{
					Lhs: []ast.Expr{ast.NewIdent("i")},
					Tok: token.DEFINE,
					Rhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}},
				},
				Cond: &ast.BinaryExpr{
					X:  ast.NewIdent("i"),
					Op: token.LSS,
					Y:  ast.NewIdent("count"),
				},
				Post: &ast.IncDecStmt{
					X:   ast.NewIdent("i"),
					Tok: token.INC,
				},
				Body: &ast.BlockStmt{
					List: []ast.Stmt{
						// fmt.Printf("Iteration %d\n", i)
						&ast.ExprStmt{
							X: &ast.CallExpr{
								Fun: &ast.SelectorExpr{
									X:   ast.NewIdent("fmt"),
									Sel: ast.NewIdent("Printf"),
								},
								Args: []ast.Expr{
									&ast.BasicLit{
										Kind:  token.STRING,
										Value: `"Iteration %d\n"`,
									},
									ast.NewIdent("i"),
								},
							},
						},
					},
				},
			},
		},
	}
	
	return funcDecl, nil
}

// BeforeBar2 hook implementation
func BeforeBar2(ctx *hooks.RuntimeHookContext) error {
	fmt.Printf("[%s] Starting bar2()\n", ctx.Function)
	return nil
}

func AfterBar2(ctx *hooks.RuntimeHookContext) error {
	fmt.Printf("[%s] Completed bar2() in %v\n", ctx.Function, ctx.Duration)
	return nil
}

// Hook implementations for main function
func BeforeMain(ctx *hooks.RuntimeHookContext) error {
	fmt.Printf("[%s] Starting main()\n", ctx.Function)
	return nil
}

func AfterMain(ctx *hooks.RuntimeHookContext) error {
	fmt.Printf("[%s] Completed main() in %v\n", ctx.Function, ctx.Duration)
	if ctx.Error != nil {
		fmt.Printf("Main function failed: %v\n", ctx.Error)
	}
	return nil
}

// Ensure HelloHookProvider implements the HookProvider interface
var _ hooks.HookProvider = (*HelloHookProvider)(nil)