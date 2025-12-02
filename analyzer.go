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
	FilePath   string // Path to the file containing this function
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
		Line:           fset.Position(call.Pos()).Line,
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

// FormatCallGraph formats the call graph for display as a unified graph with call chains
func FormatCallGraph(cg *CallGraph) string {
	var output strings.Builder

	output.WriteString("=== CALL GRAPH ===\n\n")

	// Build adjacency list for call relationships
	callGraph := make(map[string][]FunctionCall)
	callLineMap := make(map[string]map[string]int) // caller -> callee -> line number (first occurrence)

	for _, call := range cg.Calls {
		caller := call.CallerFunction
		callGraph[caller] = append(callGraph[caller], call)

		// Store line number for first occurrence
		if _, exists := callLineMap[caller]; !exists {
			callLineMap[caller] = make(map[string]int)
		}
		callee := call.CalledFunction
		if call.Package != "" {
			callee = call.Package + "." + callee
		}
		if _, exists := callLineMap[caller][callee]; !exists {
			callLineMap[caller][callee] = call.Line
		}
	}

	// Find entry points (functions that are not called by others, or main functions)
	calledFunctions := make(map[string]bool)
	for _, call := range cg.Calls {
		if call.Package == "" { // Only consider local functions
			calledFunctions[call.CalledFunction] = true
		}
	}

	entryPoints := []string{}
	allFunctions := make(map[string]bool)
	for _, call := range cg.Calls {
		allFunctions[call.CallerFunction] = true
	}

	for funcName := range allFunctions {
		if !calledFunctions[funcName] || strings.Contains(funcName, "main") {
			entryPoints = append(entryPoints, funcName)
		}
	}

	// Generate call chains for each entry point
	processedFunctions := make(map[string]bool)

	for _, entry := range entryPoints {
		if processedFunctions[entry] {
			continue
		}

		output.WriteString(fmt.Sprintf("%s:\n", entry))
		visited := make(map[string]bool)
		generateCallChains(entry, callGraph, callLineMap, "", visited, &output, processedFunctions, 1)
		output.WriteString("\n")
	}

	// Handle any remaining functions that weren't processed
	for funcName := range allFunctions {
		if !processedFunctions[funcName] {
			output.WriteString(fmt.Sprintf("%s:\n", funcName))
			visited := make(map[string]bool)
			generateCallChains(funcName, callGraph, callLineMap, "", visited, &output, processedFunctions, 1)
			output.WriteString("\n")
		}
	}

	output.WriteString(fmt.Sprintf("Summary: %d functions, %d calls\n", len(cg.Functions), len(cg.Calls)))

	return output.String()
}

// generateCallChains recursively generates call chains with proper indentation
func generateCallChains(currentFunc string, callGraph map[string][]FunctionCall,
	callLineMap map[string]map[string]int, indent string, visited map[string]bool,
	output *strings.Builder, processedFunctions map[string]bool, depth int) {

	if depth > 10 || visited[currentFunc] { // Prevent infinite recursion and limit depth
		return
	}

	visited[currentFunc] = true
	processedFunctions[currentFunc] = true

	calls, exists := callGraph[currentFunc]
	if !exists || len(calls) == 0 {
		return
	}

	// Group calls by function name to handle multiple calls to same function
	callGroups := make(map[string][]FunctionCall)
	for _, call := range calls {
		callee := call.CalledFunction
		if call.Package != "" {
			callee = call.Package + "." + callee
		}
		callGroups[callee] = append(callGroups[callee], call)
	}

	for callee, callList := range callGroups {
		// Show the call with line number
		line := callLineMap[currentFunc][callee]
		if len(callList) > 1 {
			// Multiple calls to same function, show count
			lines := make([]string, len(callList))
			for i, call := range callList {
				lines[i] = fmt.Sprintf("%d", call.Line)
			}
			output.WriteString(fmt.Sprintf("%s  -> %s (lines %s)", indent, callee, strings.Join(lines, ", ")))
		} else {
			output.WriteString(fmt.Sprintf("%s  -> %s (line %d)", indent, callee, line))
		}

		// Check if this callee has further calls (only for local functions)
		if callList[0].Package == "" && callGraph[callList[0].CalledFunction] != nil {
			// Continue the chain inline for local functions
			subVisited := make(map[string]bool)
			for k, v := range visited {
				subVisited[k] = v
			}

			subCalls := callGraph[callList[0].CalledFunction]
			if len(subCalls) > 0 {
				// Check if it's a simple single call that we can chain inline
				if len(subCalls) == 1 && subCalls[0].Package == "" && !subVisited[subCalls[0].CalledFunction] {
					// Chain inline
					nextCallee := subCalls[0].CalledFunction
					if subCalls[0].Package != "" {
						nextCallee = subCalls[0].Package + "." + nextCallee
					}
					nextLine := callLineMap[callList[0].CalledFunction][nextCallee]
					output.WriteString(fmt.Sprintf(" -> %s (line %d)", nextCallee, nextLine))

					// Continue chaining if possible
					subVisited[callList[0].CalledFunction] = true
					chainSingleCalls(subCalls[0].CalledFunction, callGraph, callLineMap, subVisited, output, 5) // Max chain length 5
				}
			}
		}

		output.WriteString("\n")

		// For complex cases with multiple calls, show them indented
		if callList[0].Package == "" && len(callGraph[callList[0].CalledFunction]) > 1 {
			subVisited := make(map[string]bool)
			for k, v := range visited {
				subVisited[k] = v
			}
			generateCallChains(callList[0].CalledFunction, callGraph, callLineMap, indent+"    ", subVisited, output, processedFunctions, depth+1)
		}
	}
}

// chainSingleCalls continues chaining single function calls inline
func chainSingleCalls(currentFunc string, callGraph map[string][]FunctionCall,
	callLineMap map[string]map[string]int, visited map[string]bool,
	output *strings.Builder, maxDepth int) {

	if maxDepth <= 0 || visited[currentFunc] {
		return
	}

	visited[currentFunc] = true
	calls := callGraph[currentFunc]

	if len(calls) == 1 && calls[0].Package == "" && !visited[calls[0].CalledFunction] {
		callee := calls[0].CalledFunction
		line := callLineMap[currentFunc][callee]
		output.WriteString(fmt.Sprintf(" -> %s (line %d)", callee, line))
		chainSingleCalls(callee, callGraph, callLineMap, visited, output, maxDepth-1)
	}
}
