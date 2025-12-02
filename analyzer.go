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
	FilePath   string   // Path to the file containing this function
}

// FunctionCall represents a function call
type FunctionCall struct {
	CallerFile     string // File containing the caller
	CallerFunction string // Function making the call
	CalledFunction string // Function being called
	Package        string // Package of the called function (if qualified)
	Line           int    // Line number of the call
}

// CallGraph represents the complete call graph
type CallGraph struct {
	Functions map[string]*FunctionInfo // Map of function signatures to FunctionInfo
	Calls     []FunctionCall           // List of function calls
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
				FilePath:   filePath,
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

// extractFunctionCallsFromGoFile extracts function calls from a Go file
func extractFunctionCallsFromGoFile(filePath string) ([]FunctionCall, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
	}

	var calls []FunctionCall
	var currentFunction string

	// Walk through the AST to find function calls
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			// Track which function we're currently in
			if x.Name != nil {
				currentFunction = x.Name.Name
				if x.Recv != nil && len(x.Recv.List) > 0 {
					// For methods, include receiver type
					recvType := extractReceiverType(x.Recv.List[0].Type)
					currentFunction = fmt.Sprintf("(%s) %s", recvType, x.Name.Name)
				}
			}
		case *ast.CallExpr:
			// Extract function call information
			if currentFunction != "" {
				call := extractCallInfo(fset, x, filePath, currentFunction)
				if call.CalledFunction != "" {
					calls = append(calls, call)
				}
			}
		}
		return true
	})

	return calls, nil
}

// extractCallInfo extracts call information from a CallExpr
func extractCallInfo(fset *token.FileSet, call *ast.CallExpr, filePath, currentFunction string) FunctionCall {
	fc := FunctionCall{
		CallerFile:     filePath,
		CallerFunction: currentFunction,
		Line:          fset.Position(call.Pos()).Line,
	}

	switch fun := call.Fun.(type) {
	case *ast.Ident:
		// Simple function call: funcName()
		fc.CalledFunction = fun.Name
	case *ast.SelectorExpr:
		// Qualified call: pkg.FuncName() or obj.Method()
		if x, ok := fun.X.(*ast.Ident); ok {
			fc.Package = x.Name
			fc.CalledFunction = fun.Sel.Name
		}
	}

	return fc
}

// BuildCallGraph builds a complete call graph from Go files
func BuildCallGraph(files []string) (*CallGraph, error) {
	cg := &CallGraph{
		Functions: make(map[string]*FunctionInfo),
		Calls:     []FunctionCall{},
	}

	// First pass: extract all function declarations
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") {
			continue
		}

		functions, err := extractFunctionsFromGoFile(file)
		if err != nil {
			fmt.Printf("Warning: Error parsing functions in %s: %v\n", file, err)
			continue
		}

		for i := range functions {
			fn := &functions[i]
			key := FormatFunctionSignature(*fn)
			cg.Functions[key] = fn
		}
	}

	// Second pass: extract all function calls
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") {
			continue
		}

		calls, err := extractFunctionCallsFromGoFile(file)
		if err != nil {
			fmt.Printf("Warning: Error parsing calls in %s: %v\n", file, err)
			continue
		}

		cg.Calls = append(cg.Calls, calls...)
	}

	return cg, nil
}

// CallPath represents a sequence of function calls
type CallPath struct {
	Functions []string // List of function names in the call path
	Files     []string // Corresponding file names
	Lines     []int    // Line numbers where calls are made
}

// findCallPaths finds all call paths starting from entry points
func findCallPaths(cg *CallGraph) []CallPath {
	// Build adjacency list for faster lookups
	callGraph := make(map[string][]FunctionCall)
	for _, call := range cg.Calls {
		caller := call.CallerFunction
		callGraph[caller] = append(callGraph[caller], call)
	}

	// Find entry points (functions that are not called by others or start with "main")
	calledFunctions := make(map[string]bool)
	for _, call := range cg.Calls {
		calledName := call.CalledFunction
		if call.Package == "" { // Only consider local functions, not external packages
			calledFunctions[calledName] = true
		}
	}

	entryPoints := []string{}
	for funcName := range cg.Functions {
		// Extract simple function name for comparison
		simpleName := extractSimpleFunctionName(funcName)
		if !calledFunctions[simpleName] || simpleName == "main" {
			entryPoints = append(entryPoints, simpleName)
		}
	}

	var allPaths []CallPath
	visited := make(map[string]bool)

	// Generate paths from each entry point
	for _, entry := range entryPoints {
		paths := generatePaths(entry, callGraph, []string{}, []string{}, []int{}, visited, 10) // Max depth 10
		allPaths = append(allPaths, paths...)
	}

	return allPaths
}

// extractSimpleFunctionName extracts just the function name from a full signature
func extractSimpleFunctionName(signature string) string {
	// Handle method signatures like "(Type) methodName(...)"
	if strings.Contains(signature, ") ") {
		parts := strings.Split(signature, ") ")
		if len(parts) > 1 {
			funcPart := parts[1]
			if idx := strings.Index(funcPart, "("); idx > 0 {
				return funcPart[:idx]
			}
		}
	}
	
	// Handle regular function signatures like "functionName(...)"
	if idx := strings.Index(signature, "("); idx > 0 {
		return signature[:idx]
	}
	
	return signature
}

// generatePaths recursively generates call paths
func generatePaths(currentFunc string, callGraph map[string][]FunctionCall, 
	currentPath []string, currentFiles []string, currentLines []int, 
	globalVisited map[string]bool, maxDepth int) []CallPath {
	
	if len(currentPath) >= maxDepth {
		return []CallPath{}
	}

	// Add current function to path
	newPath := append(currentPath, currentFunc)
	
	calls, exists := callGraph[currentFunc]
	if !exists || len(calls) == 0 {
		// End of path - return the complete path if it has at least 2 functions
		if len(newPath) >= 2 {
			return []CallPath{{
				Functions: newPath,
				Files:     currentFiles,
				Lines:     currentLines,
			}}
		}
		return []CallPath{}
	}

	var paths []CallPath
	localVisited := make(map[string]bool)
	
	for _, call := range calls {
		calledFunc := call.CalledFunction
		
		// Skip external package calls and avoid cycles
		if call.Package != "" || localVisited[calledFunc] || globalVisited[calledFunc] {
			continue
		}

		localVisited[calledFunc] = true
		
		newFiles := append(currentFiles, call.CallerFile)
		newLines := append(currentLines, call.Line)
		
		subPaths := generatePaths(calledFunc, callGraph, newPath, newFiles, newLines, globalVisited, maxDepth)
		paths = append(paths, subPaths...)
	}

	// If we found no sub-paths but have a meaningful path, return it
	if len(paths) == 0 && len(newPath) >= 2 {
		return []CallPath{{
			Functions: newPath,
			Files:     currentFiles,
			Lines:     currentLines,
		}}
	}

	return paths
}

// FormatCallGraph formats the call graph for display
func FormatCallGraph(cg *CallGraph) string {
	var output strings.Builder

	output.WriteString("=== CALL GRAPH ===\n\n")

	// Find and display call paths
	paths := findCallPaths(cg)
	
	if len(paths) > 0 {
		output.WriteString("Call Paths:\n")
		for i, path := range paths {
			if len(path.Functions) < 2 {
				continue
			}
			
			output.WriteString(fmt.Sprintf("%d. ", i+1))
			for j, funcName := range path.Functions {
				if j > 0 {
					output.WriteString(" -> ")
				}
				output.WriteString(funcName)
				
				// Add line number and file info for intermediate calls
				if j > 0 && j-1 < len(path.Lines) {
					output.WriteString(fmt.Sprintf(":%d", path.Lines[j-1]))
				}
			}
			output.WriteString("\n")
		}
	} else {
		output.WriteString("No call paths found.\n")
	}

	output.WriteString(fmt.Sprintf("\nSummary: %d functions, %d calls, %d paths\n", len(cg.Functions), len(cg.Calls), len(paths)))

	return output.String()
}
