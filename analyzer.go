package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ParameterInfo holds information about a function parameter
type ParameterInfo struct {
	Name string
	Type string
}

// FunctionInfo holds information about a function or method
type FunctionInfo struct {
	Name       string
	Receiver   string // Empty for functions, type name for methods
	Parameters []ParameterInfo
	Returns    []string // Return types
	IsExported bool
}

// extractFunctionsFromGoFile uses AST parsing to extract function and method names from a Go file
func extractFunctionsFromGoFile(filePath string) ([]FunctionInfo, error) {
	// Parse the Go source file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
	}

	var functions []FunctionInfo

	// Walk through the AST to find function and method declarations
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			info := FunctionInfo{
				Name:       x.Name.Name,
				IsExported: ast.IsExported(x.Name.Name),
			}

			// Check if it's a method (has receiver)
			if x.Recv != nil && len(x.Recv.List) > 0 {
				// Extract receiver type
				if recvType := extractReceiverType(x.Recv.List[0].Type); recvType != "" {
					info.Receiver = recvType
				}
			}

			// Extract parameters
			if x.Type.Params != nil {
				info.Parameters = extractParameters(x.Type.Params)
			}

			// Extract return types
			if x.Type.Results != nil {
				info.Returns = extractReturnTypes(x.Type.Results)
			}

			functions = append(functions, info)
		}
		return true
	})

	return functions, nil
}

// extractReceiverType extracts the receiver type name from an AST expression
func extractReceiverType(expr ast.Expr) string {
	return extractTypeString(expr)
}

// extractParameters extracts parameter information from a field list
func extractParameters(params *ast.FieldList) []ParameterInfo {
	var result []ParameterInfo

	for _, field := range params.List {
		typeStr := extractTypeString(field.Type)

		if len(field.Names) == 0 {
			// Unnamed parameter
			result = append(result, ParameterInfo{
				Name: "",
				Type: typeStr,
			})
		} else {
			// Named parameters
			for _, name := range field.Names {
				result = append(result, ParameterInfo{
					Name: name.Name,
					Type: typeStr,
				})
			}
		}
	}

	return result
}

// extractReturnTypes extracts return type strings from a field list
func extractReturnTypes(results *ast.FieldList) []string {
	var types []string

	for _, field := range results.List {
		typeStr := extractTypeString(field.Type)

		// Handle multiple return values of same type
		if len(field.Names) == 0 {
			types = append(types, typeStr)
		} else {
			for range field.Names {
				types = append(types, typeStr)
			}
		}
	}

	return types
}

// extractTypeString converts an AST expression to a type string
func extractTypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		// Pointer type
		return "*" + extractTypeString(t.X)
	case *ast.ArrayType:
		// Array or slice
		if t.Len == nil {
			return "[]" + extractTypeString(t.Elt)
		}
		return "[...]" + extractTypeString(t.Elt)
	case *ast.MapType:
		// Map type
		return "map[" + extractTypeString(t.Key) + "]" + extractTypeString(t.Value)
	case *ast.InterfaceType:
		// Interface type
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "interface{}"
		}
		return "interface{...}"
	case *ast.FuncType:
		// Function type
		return "func(...)"
	case *ast.ChanType:
		// Channel type
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + extractTypeString(t.Value)
		case ast.RECV:
			return "<-chan " + extractTypeString(t.Value)
		default:
			return "chan " + extractTypeString(t.Value)
		}
	case *ast.SelectorExpr:
		// Qualified identifier (e.g., pkg.Type)
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
	case *ast.Ellipsis:
		// Variadic parameter
		return "..." + extractTypeString(t.Elt)
	}
	return "<unknown>"
}

// FormatFunctionSignature formats a FunctionInfo into a readable signature string
func FormatFunctionSignature(fn FunctionInfo) string {
	var sig strings.Builder

	if fn.Receiver != "" {
		sig.WriteString(fmt.Sprintf("(%s) ", fn.Receiver))
	}
	sig.WriteString(fn.Name)
	sig.WriteString("(")

	// Add parameters
	for i, param := range fn.Parameters {
		if i > 0 {
			sig.WriteString(", ")
		}
		if param.Name != "" {
			sig.WriteString(param.Name + " ")
		}
		sig.WriteString(param.Type)
	}
	sig.WriteString(")")

	// Add return types
	if len(fn.Returns) > 0 {
		if len(fn.Returns) == 1 {
			sig.WriteString(" " + fn.Returns[0])
		} else {
			sig.WriteString(" (")
			sig.WriteString(strings.Join(fn.Returns, ", "))
			sig.WriteString(")")
		}
	}

	return sig.String()
}
