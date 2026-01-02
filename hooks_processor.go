package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// SourceMapping represents a mapping from original source file to instrumented file
type SourceMapping struct {
	Original     string `json:"original"`
	Instrumented string `json:"instrumented"` // WORK directory path (what's in binary debug info)
	DebugCopy    string `json:"debugCopy"`    // Permanent copy for dlv to find
	DebugDir     string `json:"debugDir"`     // Base directory of debug copies
}

// SourceMappings contains all file mappings for dlv debugger
type SourceMappings struct {
	WorkDir  string          `json:"workDir"`
	Mappings []SourceMapping `json:"mappings"`
}

// HookDefinition represents a parsed hook from the hooks file
type HookDefinition struct {
	Package  string
	Function string
	Receiver string
	Type     string // "before_after", "rewrite", or "both"

	// Rewrite-specific fields (extracted from Rewrite function AST)
	RewriteFuncName    string // Name of the Rewrite function (e.g., "RewriteNewproc1")
	RawCodeToInject    string // Raw code string to inject
	RenameReturnValues bool   // Whether to rename unnamed return values
	InjectPosition     string // "start" or "defer" - where to inject the code
}

// getHooksImportPath determines the full Go import path for a hooks file
// by finding the nearest go.mod and calculating the relative path
func getHooksImportPath(hooksFile string) (string, error) {
	absPath, err := filepath.Abs(hooksFile)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get the directory containing the hooks file
	hooksDir := filepath.Dir(absPath)

	// Find the go.mod file by walking up the directory tree
	modPath, modDir, err := findGoMod(hooksDir)
	if err != nil {
		return "", fmt.Errorf("failed to find go.mod: %w", err)
	}

	// Extract the module path from go.mod
	modulePath, err := extractModulePath(modPath)
	if err != nil {
		return "", fmt.Errorf("failed to extract module path: %w", err)
	}

	// Calculate the relative path from module root to hooks directory
	relPath, err := filepath.Rel(modDir, hooksDir)
	if err != nil {
		return "", fmt.Errorf("failed to calculate relative path: %w", err)
	}

	// Combine module path with relative path (use forward slashes for import paths)
	if relPath == "." {
		return modulePath, nil
	}
	importPath := modulePath + "/" + filepath.ToSlash(relPath)
	return importPath, nil
}

// findGoMod walks up the directory tree to find go.mod
func findGoMod(startDir string) (modPath string, modDir string, err error) {
	dir := startDir
	for {
		modPath = filepath.Join(dir, "go.mod")
		if _, err := os.Stat(modPath); err == nil {
			return modPath, dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding go.mod
			return "", "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

// extractModulePath extracts the module path from a go.mod file
func extractModulePath(modPath string) (string, error) {
	file, err := os.Open(modPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			modulePath := strings.TrimPrefix(line, "module ")
			modulePath = strings.TrimSpace(modulePath)
			return modulePath, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("module declaration not found in go.mod")
}

// parseHooksFile parses a Go file containing hook definitions and extracts hook information
func parseHooksFile(hooksFile string) ([]HookDefinition, error) {
	var hooks []HookDefinition

	// Parse the hooks file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, hooksFile, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("error parsing hooks file %s: %w", hooksFile, err)
	}

	// Find ProvideHooks function
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "ProvideHooks" {
			continue
		}

		// Parse the function body to extract hook definitions
		hooks = extractHooksFromFunction(funcDecl)
		break
	}

	if len(hooks) == 0 {
		return nil, fmt.Errorf("no hooks found in %s", hooksFile)
	}

	return hooks, nil
}

// extractHooksFromFunction extracts hook definitions from ProvideHooks function
func extractHooksFromFunction(funcDecl *ast.FuncDecl) []HookDefinition {
	var hooks []HookDefinition

	// Walk through the function body
	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		// Look for composite literals that represent Hook structs
		compLit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}

		// Check if this is a Hook struct
		hook := parseHookFromCompositeLit(compLit)
		if hook != nil {
			hooks = append(hooks, *hook)
		}

		return true
	})

	return hooks
}

// parseHookFromCompositeLit parses a Hook struct from a composite literal
func parseHookFromCompositeLit(lit *ast.CompositeLit) *HookDefinition {
	hook := &HookDefinition{}
	hasTarget := false
	hasHooks := false
	hasRewrite := false

	for _, elt := range lit.Elts {
		kvExpr, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kvExpr.Key.(*ast.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "Target":
			// Parse InjectTarget
			if targetLit, ok := kvExpr.Value.(*ast.CompositeLit); ok {
				for _, targetElt := range targetLit.Elts {
					targetKV, ok := targetElt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}

					targetKey, ok := targetKV.Key.(*ast.Ident)
					if !ok {
						continue
					}

					switch targetKey.Name {
					case "Package":
						if lit, ok := targetKV.Value.(*ast.BasicLit); ok {
							hook.Package = strings.Trim(lit.Value, `"`)
						}
					case "Function":
						if lit, ok := targetKV.Value.(*ast.BasicLit); ok {
							hook.Function = strings.Trim(lit.Value, `"`)
						}
					case "Receiver":
						if lit, ok := targetKV.Value.(*ast.BasicLit); ok {
							hook.Receiver = strings.Trim(lit.Value, `"`)
						}
					}
				}
				hasTarget = true
			}
		case "Hooks":
			// Check if Hooks field is present (not nil)
			if _, ok := kvExpr.Value.(*ast.UnaryExpr); ok {
				hasHooks = true
			}
		case "Rewrite":
			// Check if Rewrite field is present (not nil)
			if kvExpr.Value != nil {
				hasRewrite = true
				// Extract the function name if it's an identifier
				if ident, ok := kvExpr.Value.(*ast.Ident); ok {
					hook.RewriteFuncName = ident.Name
				}
			}
		}
	}

	// Determine hook type based on what's present
	if hasTarget {
		if hasHooks && hasRewrite {
			hook.Type = "both"
		} else if hasHooks {
			hook.Type = "before_after"
		} else if hasRewrite {
			hook.Type = "rewrite"
		} else {
			return nil
		}
		return hook
	}

	return nil
}

// parseRewriteFunctionsFromFile parses a hooks file and extracts rewrite information
// from all Rewrite functions (raw code to inject, whether to rename return values, etc.)
func parseRewriteFunctionsFromFile(hooksFile string, hooks []HookDefinition) []HookDefinition {
	// Parse the hooks file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, hooksFile, nil, parser.ParseComments)
	if err != nil {
		return hooks
	}

	// Create a map for quick lookup of hooks by rewrite function name
	hooksByRewriteFunc := make(map[string]*HookDefinition)
	for i := range hooks {
		if hooks[i].RewriteFuncName != "" {
			hooksByRewriteFunc[hooks[i].RewriteFuncName] = &hooks[i]
		}
	}

	// Find and parse each rewrite function
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		hook, exists := hooksByRewriteFunc[funcDecl.Name.Name]
		if !exists {
			continue
		}

		// Parse the rewrite function to extract info
		parseRewriteFunction(funcDecl, hook)
	}

	return hooks
}

// parseRewriteFunction analyzes a Rewrite function and extracts:
// - Raw code string to inject
// - Whether renameReturnValues is called
// - Injection position (start/defer)
func parseRewriteFunction(funcDecl *ast.FuncDecl, hook *HookDefinition) {
	if funcDecl.Body == nil {
		return
	}

	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			// Check for renameReturnValues call
			if ident, ok := node.Fun.(*ast.Ident); ok {
				if ident.Name == "renameReturnValues" {
					hook.RenameReturnValues = true
				}
			}

		case *ast.AssignStmt:
			// Look for rawCode := `...` assignments
			for i, lhs := range node.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					if ident.Name == "rawCode" || ident.Name == "code" || ident.Name == "deferCode" {
						if i < len(node.Rhs) {
							if lit, ok := node.Rhs[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								// Extract the raw string value
								rawCode := strings.Trim(lit.Value, "`\"")
								hook.RawCodeToInject = rawCode

								// Determine injection position based on content
								if strings.HasPrefix(strings.TrimSpace(rawCode), "defer ") {
									hook.InjectPosition = "defer"
								} else {
									hook.InjectPosition = "start"
								}
							}
						}
					}
				}
			}
		}
		return true
	})
}

// GeneratedFileDefinition represents a file to be generated into a package
type GeneratedFileDefinition struct {
	Package  string
	FileName string
	Content  string
}

// StructModificationDefinition represents a struct modification to apply
type StructModificationDefinition struct {
	Package    string
	StructName string
	AddFields  []StructFieldDefinition
}

// StructFieldDefinition represents a field to add to a struct
type StructFieldDefinition struct {
	Name string
	Type string
}

// parseGeneratedFilesFromHooksFile parses the hooks file to extract GeneratedFile definitions
// from GetGeneratedFiles() function
func parseGeneratedFilesFromHooksFile(hooksFile string) []GeneratedFileDefinition {
	var files []GeneratedFileDefinition

	// Parse the hooks file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, hooksFile, nil, parser.ParseComments)
	if err != nil {
		return files
	}

	// Find string constants that might contain generated file content
	stringConstants := make(map[string]string)
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || len(valueSpec.Names) == 0 || len(valueSpec.Values) == 0 {
				continue
			}
			if lit, ok := valueSpec.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				stringConstants[valueSpec.Names[0].Name] = strings.Trim(lit.Value, "`\"")
			}
		}
	}

	// Find GetGeneratedFiles function
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "GetGeneratedFiles" {
			continue
		}

		// Parse the function body to extract GeneratedFile definitions
		ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
			compLit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}

			file := parseGeneratedFileFromCompositeLit(compLit, stringConstants)
			if file != nil {
				files = append(files, *file)
			}

			return true
		})
		break
	}

	return files
}

// parseGeneratedFileFromCompositeLit parses a GeneratedFile struct from a composite literal
func parseGeneratedFileFromCompositeLit(lit *ast.CompositeLit, stringConstants map[string]string) *GeneratedFileDefinition {
	file := &GeneratedFileDefinition{}
	hasPackage := false
	hasFileName := false

	for _, elt := range lit.Elts {
		kvExpr, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kvExpr.Key.(*ast.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "Package":
			if lit, ok := kvExpr.Value.(*ast.BasicLit); ok {
				file.Package = strings.Trim(lit.Value, `"`)
				hasPackage = true
			}
		case "FileName":
			if lit, ok := kvExpr.Value.(*ast.BasicLit); ok {
				file.FileName = strings.Trim(lit.Value, `"`)
				hasFileName = true
			}
		case "Content":
			// Content can be a string literal or a constant reference
			if lit, ok := kvExpr.Value.(*ast.BasicLit); ok {
				file.Content = strings.Trim(lit.Value, "`\"")
			} else if ident, ok := kvExpr.Value.(*ast.Ident); ok {
				// It's a reference to a constant
				if content, exists := stringConstants[ident.Name]; exists {
					file.Content = content
				}
			}
		}
	}

	if hasPackage && hasFileName && file.Content != "" {
		return file
	}

	return nil
}

// parseStructModificationsFromHooksFile parses the hooks file to extract StructModification definitions
// from GetStructModifications() function
func parseStructModificationsFromHooksFile(hooksFile string) []StructModificationDefinition {
	var modifications []StructModificationDefinition

	// Parse the hooks file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, hooksFile, nil, parser.ParseComments)
	if err != nil {
		return modifications
	}

	// Find GetStructModifications function
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "GetStructModifications" {
			continue
		}

		// Parse the function body to extract StructModification definitions
		ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
			compLit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}

			mod := parseStructModificationFromCompositeLit(compLit)
			if mod != nil {
				modifications = append(modifications, *mod)
			}

			return true
		})
		break
	}

	return modifications
}

// parseStructModificationFromCompositeLit parses a StructModification struct from a composite literal
func parseStructModificationFromCompositeLit(lit *ast.CompositeLit) *StructModificationDefinition {
	mod := &StructModificationDefinition{}
	hasPackage := false
	hasStructName := false

	for _, elt := range lit.Elts {
		kvExpr, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kvExpr.Key.(*ast.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "Package":
			if basicLit, ok := kvExpr.Value.(*ast.BasicLit); ok {
				mod.Package = strings.Trim(basicLit.Value, `"`)
				hasPackage = true
			}
		case "StructName":
			if basicLit, ok := kvExpr.Value.(*ast.BasicLit); ok {
				mod.StructName = strings.Trim(basicLit.Value, `"`)
				hasStructName = true
			}
		case "AddFields":
			// Parse the slice of StructField
			if compLit, ok := kvExpr.Value.(*ast.CompositeLit); ok {
				for _, fieldElt := range compLit.Elts {
					if fieldLit, ok := fieldElt.(*ast.CompositeLit); ok {
						field := parseStructFieldFromCompositeLit(fieldLit)
						if field != nil {
							mod.AddFields = append(mod.AddFields, *field)
						}
					}
				}
			}
		}
	}

	if hasPackage && hasStructName && len(mod.AddFields) > 0 {
		return mod
	}

	return nil
}

// parseStructFieldFromCompositeLit parses a StructField from a composite literal
func parseStructFieldFromCompositeLit(lit *ast.CompositeLit) *StructFieldDefinition {
	field := &StructFieldDefinition{}
	hasName := false
	hasType := false

	for _, elt := range lit.Elts {
		kvExpr, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kvExpr.Key.(*ast.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "Name":
			if basicLit, ok := kvExpr.Value.(*ast.BasicLit); ok {
				field.Name = strings.Trim(basicLit.Value, `"`)
				hasName = true
			}
		case "Type":
			if basicLit, ok := kvExpr.Value.(*ast.BasicLit); ok {
				field.Type = strings.Trim(basicLit.Value, `"`)
				hasType = true
			}
		}
	}

	if hasName && hasType {
		return field
	}

	return nil
}

// applyStructModification modifies a struct definition in a source file by adding new fields
func applyStructModification(sourceFile string, targetFile string, mod StructModificationDefinition) error {
	// Parse the source file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse source file %s: %w", sourceFile, err)
	}

	modified := false

	// Find the struct type declaration
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != mod.StructName {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// Add new fields to the struct
			for _, field := range mod.AddFields {
				newField := &ast.Field{
					Names: []*ast.Ident{ast.NewIdent(field.Name)},
					Type:  ast.NewIdent(field.Type),
				}
				structType.Fields.List = append(structType.Fields.List, newField)
				fmt.Printf("           ➕ Added field '%s %s' to struct '%s'\n", field.Name, field.Type, mod.StructName)
			}
			modified = true
			break
		}

		if modified {
			break
		}
	}

	if !modified {
		return fmt.Errorf("struct '%s' not found in file %s", mod.StructName, sourceFile)
	}

	// Write the modified file
	file, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("failed to create target file %s: %w", targetFile, err)
	}
	defer file.Close()

	if err := format.Node(file, fset, node); err != nil {
		return fmt.Errorf("failed to format and write modified file: %w", err)
	}

	return nil
}

// writeGeneratedFileToPackage writes a generated file to the appropriate package directory in WORK
func writeGeneratedFileToPackage(genFile GeneratedFileDefinition, workDir string, buildID string) (string, error) {
	if workDir == "" || buildID == "" {
		return "", fmt.Errorf("missing work directory or build ID")
	}

	// Create the target directory: $WORK/buildID/
	targetDir := filepath.Join(workDir, buildID)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	// Write the generated file
	targetFile := filepath.Join(targetDir, genFile.FileName)
	if err := os.WriteFile(targetFile, []byte(genFile.Content), 0644); err != nil {
		return "", fmt.Errorf("failed to write generated file %s: %w", targetFile, err)
	}

	fmt.Printf("           📝 Generated file: %s\n", targetFile)
	return targetFile, nil
}

// findStructDefinitionFile finds the source file containing a struct definition
func findStructDefinitionFile(files []string, structName string) (string, error) {
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") {
			continue
		}

		// Parse the file
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			continue
		}

		// Look for the struct type declaration
		for _, decl := range node.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				if typeSpec.Name.Name == structName {
					if _, ok := typeSpec.Type.(*ast.StructType); ok {
						return file, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("struct '%s' not found in any file", structName)
}

// matchFunctionWithHooks checks if a function matches any of the provided hooks
func matchFunctionWithHooks(packageName string, funcInfo *FunctionInfo, hooks []HookDefinition) *HookDefinition {
	for _, hook := range hooks {
		// Match package name
		if hook.Package != packageName {
			continue
		}

		// Match function name
		if hook.Function != funcInfo.Name {
			continue
		}

		// Match receiver (if any)
		if hook.Receiver != "" && hook.Receiver != funcInfo.Receiver {
			continue
		}

		// If receiver is empty in hook but function has receiver, skip
		if hook.Receiver == "" && funcInfo.Receiver != "" {
			continue
		}

		return &hook
	}

	return nil
}

// processCompileWithHooks processes compile commands and matches them against hooks
func processCompileWithHooks(commands []Command, hooksFile string) error {
	// Parse the hooks file
	hooks, err := parseHooksFile(hooksFile)
	if err != nil {
		// It's ok if no hooks are found - we might still have struct modifications or generated files
		fmt.Printf("⚠️  Warning: %v\n", err)
		hooks = []HookDefinition{}
	}

	// Parse rewrite functions to extract raw code and transformation info
	hooks = parseRewriteFunctionsFromFile(hooksFile, hooks)

	// Parse struct modifications from the hooks file
	structMods := parseStructModificationsFromHooksFile(hooksFile)
	if len(structMods) > 0 {
		fmt.Printf("Loaded %d struct modifications from %s\n", len(structMods), filepath.Base(hooksFile))
		for _, mod := range structMods {
			fmt.Printf("  - Package: %s, Struct: %s, Fields to add: %d\n", mod.Package, mod.StructName, len(mod.AddFields))
		}
	}

	// Parse generated files from the hooks file
	generatedFiles := parseGeneratedFilesFromHooksFile(hooksFile)
	if len(generatedFiles) > 0 {
		fmt.Printf("Loaded %d generated files from %s\n", len(generatedFiles), filepath.Base(hooksFile))
		for _, gf := range generatedFiles {
			fmt.Printf("  - Package: %s, File: %s (%d bytes)\n", gf.Package, gf.FileName, len(gf.Content))
		}
	}

	// Get the full import path for the hooks package
	hooksImportPath, err := getHooksImportPath(hooksFile)
	if err != nil {
		fmt.Printf("⚠️  Warning: Could not determine hooks import path: %v\n", err)
		fmt.Printf("   Using package name only for go:linkname (may not work)\n")
		hooksImportPath = "generated_hooks" // Fallback
	} else {
		fmt.Printf("Hooks import path: %s\n", hooksImportPath)
	}

	fmt.Printf("=== Compile Mode with Hooks ===\n")
	fmt.Printf("Loaded %d hook definitions from %s\n\n", len(hooks), filepath.Base(hooksFile))

	// Get package path information using existing functionality
	packageInfo := extractPackagePathInfo(commands)

	// Extract work directory
	workDir := extractWorkDirFromCommands(commands)
	if workDir != "" {
		fmt.Printf("Work directory: %s\n", workDir)
	}

	// Display loaded hooks
	fmt.Println("Hook Definitions:")
	for _, hook := range hooks {
		fmt.Printf("  - Package: %s, Function: %s", hook.Package, hook.Function)
		if hook.Receiver != "" {
			fmt.Printf(", Receiver: %s", hook.Receiver)
		}
		fmt.Printf(" [%s]\n", hook.Type)
	}
	fmt.Println()

	compileCount := 0
	matchCount := 0
	packagesWithMatches := make(map[string]bool)      // Track packages that have matches
	copiedFiles := make(map[string]bool)              // Track files already copied per package
	fileReplacements := make(map[string]string)       // Track original file -> instrumented file mapping
	trampolineFiles := make(map[string]string)        // Track package -> trampolines file path
	generatedFilePaths := make(map[string][]string)   // Track package -> generated file paths
	structModApplied := make(map[string]bool)         // Track which struct modifications have been applied
	packagesWithStructMods := make(map[string]bool)   // Track packages with struct modifications

	// Process each compile command
	for cmdIdx, cmd := range commands {
		if !isCompileCommand(&cmd) {
			continue
		}

		compileCount++
		packageName := extractPackageName(&cmd)
		files := extractPackFiles(&cmd)

		if packageName == "" || len(files) == 0 {
			continue
		}

		fmt.Printf("Command %d: Package '%s' with %d files\n", cmdIdx+1, packageName, len(files))

		packageHasMatches := false

		// Process each Go file
		for _, file := range files {
			if !strings.HasSuffix(file, ".go") {
				continue
			}

			functions, err := extractFunctionsFromGoFile(file)
			if err != nil {
				fmt.Printf("  Error parsing %s: %v\n", file, err)
				continue
			}

			fileHasMatches := false
			fileNeedsTrampolines := false
			fileNeedsRewrite := false

			// Check each function against hooks
			for _, fn := range functions {
				if match := matchFunctionWithHooks(packageName, &fn, hooks); match != nil {
					matchCount++
					packageHasMatches = true
					fileHasMatches = true
					fmt.Printf("  ✓ MATCH: %s:%s", filepath.Base(file), fn.Name)
					if fn.Receiver != "" {
						fmt.Printf(" (receiver: %s)", fn.Receiver)
					}
					fmt.Printf(" -> Hook type: %s\n", match.Type)

					// Show what will happen
					switch match.Type {
					case "before_after":
						fmt.Printf("           Will inject: Before and After hooks\n")
						fileNeedsTrampolines = true
					case "rewrite":
						fmt.Printf("           Will rewrite: Function body (inject raw code)\n")
						if match.RawCodeToInject != "" {
							fmt.Printf("           Raw code to inject: %d bytes\n", len(match.RawCodeToInject))
						}
						fileNeedsRewrite = true
					case "both":
						fmt.Printf("           Will inject: Before/After hooks AND rewrite function\n")
						fileNeedsTrampolines = true
						fileNeedsRewrite = true
					}
				}
			}

			// Copy and instrument the source file to work directory if it has matches and hasn't been copied yet
			// Process files that need trampolines OR rewrite
			if fileHasMatches && (fileNeedsTrampolines || fileNeedsRewrite) && workDir != "" {
				copyKey := packageName + ":" + file
				if !copiedFiles[copyKey] {
					if pkgInfo, exists := packageInfo[packageName]; exists && pkgInfo.BuildID != "" {
						instrumentedFilePath := filepath.Join(workDir, pkgInfo.BuildID, filepath.Base(file))
						if err := copyAndInstrumentFileOnly(file, workDir, pkgInfo.BuildID, packageName, hooks, hooksImportPath); err != nil {
							fmt.Printf("           ⚠️  Failed to copy and instrument file: %v\n", err)
						} else {
							copiedFiles[copyKey] = true
							// Track the file replacement mapping - only for Go files
							if strings.HasSuffix(file, ".go") {
								fileReplacements[file] = instrumentedFilePath
								fmt.Printf("           🔄 Will replace %s with %s in compile command\n", file, instrumentedFilePath)

								// Track the trampolines file for this package - only for before_after hooks
								if fileNeedsTrampolines {
									trampolinesPath := filepath.Join(workDir, pkgInfo.BuildID, "otel_trampolines.go")
									trampolineFiles[packageName] = trampolinesPath
								}
							}
						}
					}
				}
			}
		}

		// Mark this package as having matches
		if packageHasMatches {
			packagesWithMatches[packageName] = true
		}

		// Check for struct modifications in this package
		for _, mod := range structMods {
			if mod.Package != packageName {
				continue
			}

			modKey := mod.Package + ":" + mod.StructName
			if structModApplied[modKey] {
				continue
			}

			fmt.Printf("  🔍 Looking for struct '%s' to modify in package '%s'\n", mod.StructName, packageName)

			// Find the file containing the struct definition
			structFile, err := findStructDefinitionFile(files, mod.StructName)
			if err != nil {
				fmt.Printf("     ⚠️  %v\n", err)
				continue
			}

			fmt.Printf("     Found struct in: %s\n", filepath.Base(structFile))

			// Apply struct modification
			if pkgInfo, exists := packageInfo[packageName]; exists && pkgInfo.BuildID != "" && workDir != "" {
				targetDir := filepath.Join(workDir, pkgInfo.BuildID)
				if err := os.MkdirAll(targetDir, 0755); err != nil {
					fmt.Printf("     ⚠️  Failed to create target dir: %v\n", err)
					continue
				}

				targetFile := filepath.Join(targetDir, filepath.Base(structFile))
				if err := applyStructModification(structFile, targetFile, mod); err != nil {
					fmt.Printf("     ⚠️  Failed to apply struct modification: %v\n", err)
				} else {
					structModApplied[modKey] = true
					packagesWithStructMods[packageName] = true

					// Track the file replacement
					fileReplacements[structFile] = targetFile
					fmt.Printf("     ✅ Modified struct '%s' and saved to: %s\n", mod.StructName, targetFile)
				}
			}
		}

		// Check for generated files for this package
		for _, genFile := range generatedFiles {
			if genFile.Package != packageName {
				continue
			}

			fmt.Printf("  📝 Generating file '%s' for package '%s'\n", genFile.FileName, packageName)

			if pkgInfo, exists := packageInfo[packageName]; exists && pkgInfo.BuildID != "" && workDir != "" {
				genFilePath, err := writeGeneratedFileToPackage(genFile, workDir, pkgInfo.BuildID)
				if err != nil {
					fmt.Printf("     ⚠️  Failed to generate file: %v\n", err)
				} else {
					generatedFilePaths[packageName] = append(generatedFilePaths[packageName], genFilePath)
					packagesWithMatches[packageName] = true // Ensure this package gets processed
					fmt.Printf("     ✅ Generated: %s\n", genFilePath)
				}
			}
		}
	}

	// Suppress unused variable warnings
	_ = packagesWithStructMods

	fmt.Printf("\nSummary: Processed %d compile commands, found %d hook matches in %d packages\n",
		compileCount, matchCount, len(packagesWithMatches))

	if len(packagesWithMatches) > 0 {
		fmt.Println("Packages with hook matches:")
		for pkg := range packagesWithMatches {
			if info, exists := packageInfo[pkg]; exists {
				fmt.Printf("  - %s (BuildID: %s, Path: %s)\n", pkg, info.BuildID, info.Path)
			} else {
				fmt.Printf("  - %s (no build info found)\n", pkg)
			}
		}
	}

	// Find the main package compile command and generate otel.runtime.go
	var mainPackageInfo *PackagePathInfo
	var mainBuildID string
	for _, cmd := range commands {
		if isCompileCommand(&cmd) {
			pkgName := extractPackageName(&cmd)
			if pkgName == "main" {
				if info, exists := packageInfo[pkgName]; exists {
					mainPackageInfo = &info
					mainBuildID = info.BuildID
					fmt.Printf("Found main package with BuildID: %s\n", mainBuildID)
				}
				break
			}
		}
	}

	// Generate otel.runtime.go for main package if we have matches
	var otelRuntimeFile string
	if len(fileReplacements) > 0 && workDir != "" && mainBuildID != "" {
		runtimeDir := filepath.Join(workDir, mainBuildID)
		if err := os.MkdirAll(runtimeDir, 0755); err == nil {
			var err error
			otelRuntimeFile, err = generateOtelRuntimeFile(runtimeDir, hooksImportPath)
			if err != nil {
				fmt.Printf("⚠️  Failed to generate otel.runtime.go: %v\n", err)
			} else {
				fmt.Printf("📄 Generated otel.runtime.go: %s\n", otelRuntimeFile)
			}
		}
	}

	// Generate modified build log with updated file paths
	if len(fileReplacements) > 0 || len(generatedFilePaths) > 0 {
		if err := generateModifiedBuildLog(commands, fileReplacements, trampolineFiles, generatedFilePaths, hooksImportPath, workDir, hooksFile, otelRuntimeFile, mainPackageInfo); err != nil {
			fmt.Printf("⚠️  Failed to generate modified build log: %v\n", err)
		} else {
			fmt.Printf("\n📄 Generated modified build log: go-build-modified.log\n")

			// Save source mappings for dlv debugger
			if err := saveSourceMappings(fileReplacements, workDir); err != nil {
				fmt.Printf("⚠️  Failed to save source mappings: %v\n", err)
			} else {
				fmt.Printf("📄 Generated source mappings: source-mappings.json\n")
			}

			// Execute commands from the modified build log using existing functionality
			fmt.Printf("\n🚀 Executing commands from modified build log...\n")
			if err := executeModifiedBuildLogWithParser("go-build-modified.log"); err != nil {
				fmt.Printf("⚠️  Failed to execute modified build log: %v\n", err)
			} else {
				fmt.Printf("✅ Successfully executed all commands from modified build log\n")
			}
		}
	}

	return nil
}

// extractWorkDirFromCommands extracts the work directory from commands
func extractWorkDirFromCommands(commands []Command) string {
	for _, cmd := range commands {
		if workDir := extractWorkDir(cmd.Raw); workDir != "" {
			return workDir
		}
	}
	return ""
}

// saveSourceMappings saves the file mappings to source-mappings.json for dlv debugger
// It reads the WORK directory from go-build.log (matching what's in the compiled binary)
// and copies instrumented source files to a permanent location (.otel-build/debug/).
// The mappings contain:
// - original: the original source file path
// - instrumented: the WORK directory path (what's compiled into the binary)
// - debugCopy: permanent copy of the instrumented file for dlv to find
func saveSourceMappings(fileReplacements map[string]string, currentWorkDir string) error {
	// Read the WORK directory from go-build.log (this matches what's in the binary)
	workDir := getWorkDirFromBuildLog()
	if workDir == "" {
		// Fall back to current work dir if go-build.log not found
		workDir = currentWorkDir
		fmt.Printf("⚠️  Could not read WORK dir from go-build.log, using current: %s\n", workDir)
	} else {
		fmt.Printf("📍 Using WORK directory from go-build.log: %s\n", workDir)
	}

	// Create permanent directory for instrumented sources
	debugDir := ".otel-build/debug"
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return fmt.Errorf("failed to create debug directory: %w", err)
	}

	mappings := SourceMappings{
		WorkDir:  workDir,
		Mappings: make([]SourceMapping, 0, len(fileReplacements)),
	}

	for original, instrumented := range fileReplacements {
		// Convert relative paths to absolute paths
		absOriginal := original
		if !filepath.IsAbs(original) {
			if abs, err := filepath.Abs(original); err == nil {
				absOriginal = abs
			}
		}

		// Extract the relative path from WORK directory (e.g., b001/src/main.go)
		// Use current work dir to extract relative path, then apply to saved work dir
		relPath := strings.TrimPrefix(instrumented, currentWorkDir)
		relPath = strings.TrimPrefix(relPath, "/")

		// The instrumented path as recorded in the binary's debug info
		binaryInstrumentedPath := filepath.Join(workDir, relPath)

		// Permanent copy location
		permanentPath := filepath.Join(debugDir, relPath)

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(permanentPath), 0755); err != nil {
			fmt.Printf("⚠️  Failed to create directory for %s: %v\n", permanentPath, err)
			continue
		}

		// Read instrumented file and copy to permanent location
		content, err := os.ReadFile(instrumented)
		if err != nil {
			fmt.Printf("⚠️  Failed to read instrumented file %s: %v\n", instrumented, err)
			continue
		}
		if err := os.WriteFile(permanentPath, content, 0644); err != nil {
			fmt.Printf("⚠️  Failed to write instrumented file to %s: %v\n", permanentPath, err)
			continue
		}

		// Get absolute path for the permanent location
		absPermanentPath, err := filepath.Abs(permanentPath)
		if err != nil {
			absPermanentPath = permanentPath
		}

		// Get absolute path for debug directory
		absDebugDir, err := filepath.Abs(debugDir)
		if err != nil {
			absDebugDir = debugDir
		}

		fmt.Printf("📋 Copied instrumented source: %s -> %s\n", filepath.Base(original), absPermanentPath)
		fmt.Printf("   Binary debug path: %s\n", binaryInstrumentedPath)

		mappings.Mappings = append(mappings.Mappings, SourceMapping{
			Original:     absOriginal,
			Instrumented: binaryInstrumentedPath, // WORK directory path from go-build.log
			DebugCopy:    absPermanentPath,
			DebugDir:     absDebugDir,
		})
	}

	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal source mappings: %w", err)
	}

	if err := os.WriteFile("source-mappings.json", data, 0644); err != nil {
		return fmt.Errorf("failed to write source-mappings.json: %w", err)
	}

	return nil
}

// getWorkDirFromBuildLog reads the WORK directory from go-build.log
func getWorkDirFromBuildLog() string {
	file, err := os.Open("go-build.log")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if workDir := extractWorkDir(line); workDir != "" {
			return workDir
		}
	}
	return ""
}

// generateSourceMappingsFromExisting generates source-mappings.json from existing build files
// without running a new compile. It reads the WORK directory from go-build.log and
// extracts file mappings from go-build-modified.log.
func generateSourceMappingsFromExisting() error {
	// Read WORK directory from go-build.log
	workDir := getWorkDirFromBuildLog()
	if workDir == "" {
		return fmt.Errorf("could not find WORK directory in go-build.log")
	}
	fmt.Printf("📍 Found WORK directory: %s\n", workDir)

	// Parse go-build-modified.log to find instrumented files
	// Look for lines that reference the WORK directory with .go files
	modifiedLog, err := os.Open("go-build-modified.log")
	if err != nil {
		return fmt.Errorf("could not open go-build-modified.log: %w", err)
	}
	defer modifiedLog.Close()

	// Create debug directory
	debugDir := ".otel-build/debug"
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return fmt.Errorf("failed to create debug directory: %w", err)
	}

	mappings := SourceMappings{
		WorkDir:  workDir,
		Mappings: make([]SourceMapping, 0),
	}

	// Track unique files we've already processed
	processedFiles := make(map[string]bool)

	scanner := bufio.NewScanner(modifiedLog)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Look for .go files in the WORK directory
		// Pattern: files in $WORK/bXXX/src/*.go
		if !strings.Contains(line, workDir) {
			continue
		}

		// Extract .go file paths from the line
		parts := strings.Fields(line)
		for _, part := range parts {
			if !strings.HasSuffix(part, ".go") {
				continue
			}
			if !strings.HasPrefix(part, workDir) {
				continue
			}
			if processedFiles[part] {
				continue
			}

			// This is an instrumented file path
			instrumentedPath := part
			processedFiles[instrumentedPath] = true

			// Extract relative path from WORK dir
			relPath := strings.TrimPrefix(instrumentedPath, workDir)
			relPath = strings.TrimPrefix(relPath, "/")

			// Skip trampolines and runtime files
			baseName := filepath.Base(relPath)
			if baseName == "otel_trampolines.go" || baseName == "otel.runtime.go" {
				continue
			}

			// Try to find the original file (same base name in current directory)
			originalPath := baseName
			if abs, err := filepath.Abs(originalPath); err == nil {
				originalPath = abs
			}

			// Permanent copy location
			permanentPath := filepath.Join(debugDir, relPath)

			// Create parent directories
			if err := os.MkdirAll(filepath.Dir(permanentPath), 0755); err != nil {
				fmt.Printf("⚠️  Failed to create directory for %s: %v\n", permanentPath, err)
				continue
			}

			// Try to copy the instrumented file from WORK dir (might still exist)
			// or from existing debug copy
			var content []byte
			var copyErr error

			// First try the WORK directory
			content, copyErr = os.ReadFile(instrumentedPath)
			if copyErr != nil {
				// Try existing debug copy
				content, copyErr = os.ReadFile(permanentPath)
			}

			if copyErr != nil {
				fmt.Printf("⚠️  Could not find source for %s (WORK dir may have been cleaned)\n", baseName)
				// Still add the mapping even without the file
			} else {
				if err := os.WriteFile(permanentPath, content, 0644); err != nil {
					fmt.Printf("⚠️  Failed to write %s: %v\n", permanentPath, err)
				}
			}

			absPermanentPath, _ := filepath.Abs(permanentPath)
			absDebugDir, _ := filepath.Abs(debugDir)

			fmt.Printf("📋 Mapping: %s -> %s\n", baseName, instrumentedPath)

			mappings.Mappings = append(mappings.Mappings, SourceMapping{
				Original:     originalPath,
				Instrumented: instrumentedPath,
				DebugCopy:    absPermanentPath,
				DebugDir:     absDebugDir,
			})
		}
	}

	if len(mappings.Mappings) == 0 {
		return fmt.Errorf("no instrumented files found in go-build-modified.log")
	}

	// Write source-mappings.json
	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal source mappings: %w", err)
	}

	if err := os.WriteFile("source-mappings.json", data, 0644); err != nil {
		return fmt.Errorf("failed to write source-mappings.json: %w", err)
	}

	fmt.Printf("✅ Generated source-mappings.json with %d mappings\n", len(mappings.Mappings))
	return nil
}

// instrumentFile instruments a Go file with trampoline functions and calls
func instrumentFile(sourceFile, targetFile string, packageName string, hooks []HookDefinition, hooksImportPath string) error {
	// Parse the source file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse source file %s: %w", sourceFile, err)
	}

	// Get the actual package name from the AST
	actualPackageName := node.Name.Name

	// Track which hooks apply to functions in this file
	var applicableHooks []HookDefinition
	var instrumentedFunctions []string
	var rewrittenFunctions []string

	// Find functions that match hooks
	for _, decl := range node.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			funcInfo := &FunctionInfo{
				Name:     funcDecl.Name.Name,
				Receiver: "",
			}

			// Extract receiver if it's a method
			if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
				if ident, ok := funcDecl.Recv.List[0].Type.(*ast.Ident); ok {
					funcInfo.Receiver = ident.Name
				}
			}

			// Check if this function matches any hook
			if match := matchFunctionWithHooks(packageName, funcInfo, hooks); match != nil {
				switch match.Type {
				case "before_after":
					applicableHooks = append(applicableHooks, *match)
					instrumentedFunctions = append(instrumentedFunctions, funcDecl.Name.Name)
					instrumentFunction(funcDecl, match)

				case "rewrite":
					if err := applyRewriteTransformation(funcDecl, match); err != nil {
						fmt.Printf("           ⚠️  Failed to apply rewrite to %s: %v\n", funcDecl.Name.Name, err)
					} else {
						rewrittenFunctions = append(rewrittenFunctions, funcDecl.Name.Name)
					}

				case "both":
					// First apply rewrite, then add hooks
					if err := applyRewriteTransformation(funcDecl, match); err != nil {
						fmt.Printf("           ⚠️  Failed to apply rewrite to %s: %v\n", funcDecl.Name.Name, err)
					} else {
						rewrittenFunctions = append(rewrittenFunctions, funcDecl.Name.Name)
					}
					applicableHooks = append(applicableHooks, *match)
					instrumentedFunctions = append(instrumentedFunctions, funcDecl.Name.Name)
					instrumentFunction(funcDecl, match)
				}
			}
		}
	}

	// Write the instrumented file
	file, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("failed to create target file %s: %w", targetFile, err)
	}
	defer file.Close()

	if err := format.Node(file, fset, node); err != nil {
		return fmt.Errorf("failed to format and write instrumented file: %w", err)
	}

	// Generate separate trampolines file if we have applicable hooks
	if len(applicableHooks) > 0 {
		targetDir := filepath.Dir(targetFile)
		trampolinesFile := filepath.Join(targetDir, "otel_trampolines.go")
		if err := generateTrampolinesFile(trampolinesFile, actualPackageName, applicableHooks, hooksImportPath); err != nil {
			return fmt.Errorf("failed to generate trampolines file: %w", err)
		}
		fmt.Printf("           📄 Generated trampolines file: %s\n", trampolinesFile)
	}

	if len(instrumentedFunctions) > 0 {
		fmt.Printf("           🔧 Instrumented functions: %s\n", strings.Join(instrumentedFunctions, ", "))
	}

	if len(rewrittenFunctions) > 0 {
		fmt.Printf("           ✏️  Rewritten functions: %s\n", strings.Join(rewrittenFunctions, ", "))
	}

	return nil
}

// applyRewriteTransformation applies the rewrite transformation to a function
// based on the extracted RawCodeToInject and other settings
func applyRewriteTransformation(funcDecl *ast.FuncDecl, hook *HookDefinition) error {
	if funcDecl.Body == nil {
		return fmt.Errorf("function %s has no body", funcDecl.Name.Name)
	}

	if hook.RawCodeToInject == "" {
		return fmt.Errorf("no raw code to inject for function %s", funcDecl.Name.Name)
	}

	// Rename unnamed return values if needed
	if hook.RenameReturnValues {
		renameUnnamedReturnValues(funcDecl)
	}

	// Parse the raw code to inject
	stmts, err := parseCodeSnippet(hook.RawCodeToInject)
	if err != nil {
		return fmt.Errorf("failed to parse raw code: %w", err)
	}

	// Insert at the beginning of the function body
	funcDecl.Body.List = append(stmts, funcDecl.Body.List...)

	return nil
}

// renameUnnamedReturnValues renames unnamed return values to _unnamedRetVal0, _unnamedRetVal1, etc.
func renameUnnamedReturnValues(funcDecl *ast.FuncDecl) {
	if funcDecl.Type.Results == nil {
		return
	}
	idx := 0
	for _, field := range funcDecl.Type.Results.List {
		if field.Names == nil || len(field.Names) == 0 {
			name := fmt.Sprintf("_unnamedRetVal%d", idx)
			field.Names = []*ast.Ident{ast.NewIdent(name)}
			idx++
		}
	}
}

// parseCodeSnippet parses a code snippet into AST statements
func parseCodeSnippet(code string) ([]ast.Stmt, error) {
	// Wrap in a function to make it parseable
	wrapped := fmt.Sprintf("package p\nfunc f() {\n%s\n}", code)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", wrapped, 0)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Extract statements from the function body
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn.Body.List, nil
		}
	}
	return nil, fmt.Errorf("no function found in parsed snippet")
}

// generateTrampolinesFile creates a separate file with trampoline functions and go:linkname declarations
func generateTrampolinesFile(targetFile string, packageName string, hooks []HookDefinition, hooksImportPath string) error {
	var sb strings.Builder

	// Write package declaration
	sb.WriteString(fmt.Sprintf("package %s\n\n", packageName))

	// Write imports - unsafe for go:linkname and hooks for HookContext
	sb.WriteString(`import (
	_ "unsafe" // Required for go:linkname

	"github.com/pdelewski/go-build-interceptor/hooks"
)

`)

	fmt.Printf("           🔗 Using go:linkname to link to: %s\n", hooksImportPath)

	// Generate trampolines for each hook
	for _, hook := range hooks {
		pascalName := capitalizeFirst(hook.Function)

		// HookContextImpl struct - implements hooks.HookContext
		sb.WriteString(fmt.Sprintf(`// HookContextImpl%s implements hooks.HookContext for %s
type HookContextImpl%s struct {
	data        interface{}
	skipCall    bool
	funcName    string
	packageName string
}

func (c *HookContextImpl%s) SetData(data interface{}) { c.data = data }
func (c *HookContextImpl%s) GetData() interface{}     { return c.data }
func (c *HookContextImpl%s) SetSkipCall(skip bool)    { c.skipCall = skip }
func (c *HookContextImpl%s) IsSkipCall() bool         { return c.skipCall }
func (c *HookContextImpl%s) GetFuncName() string      { return c.funcName }
func (c *HookContextImpl%s) GetPackageName() string   { return c.packageName }

func (c *HookContextImpl%s) GetKeyData(key string) interface{} {
	if c.data == nil {
		return nil
	}
	if m, ok := c.data.(map[string]interface{}); ok {
		return m[key]
	}
	return nil
}

func (c *HookContextImpl%s) SetKeyData(key string, val interface{}) {
	if c.data == nil {
		c.data = make(map[string]interface{})
	}
	if m, ok := c.data.(map[string]interface{}); ok {
		m[key] = val
	}
}

func (c *HookContextImpl%s) HasKeyData(key string) bool {
	if c.data == nil {
		return false
	}
	if m, ok := c.data.(map[string]interface{}); ok {
		_, ok := m[key]
		return ok
	}
	return false
}

`, pascalName, hook.Function,
			pascalName,
			pascalName, pascalName, pascalName, pascalName, pascalName, pascalName,
			pascalName, pascalName, pascalName))

		// Before trampoline - calls the go:linkname function
		sb.WriteString(fmt.Sprintf(`// OtelBeforeTrampoline_%s is the before trampoline for %s
func OtelBeforeTrampoline_%s() (hookContext *HookContextImpl%s, skipCall bool) {
	defer func() {
		if err := recover(); err != nil {
			println("failed to exec Before hook", "Before%s")
		}
	}()
	hookContext = &HookContextImpl%s{}
	hookContext.funcName = "%s"
	hookContext.packageName = "%s"
	Before%s(hookContext)
	return hookContext, hookContext.skipCall
}

`, pascalName, hook.Function,
			pascalName, pascalName,
			pascalName,
			pascalName,
			hook.Function, hook.Package,
			pascalName))

		// After trampoline - calls the go:linkname function
		sb.WriteString(fmt.Sprintf(`// OtelAfterTrampoline_%s is the after trampoline for %s
func OtelAfterTrampoline_%s(hookContext hooks.HookContext) {
	defer func() {
		if err := recover(); err != nil {
			println("failed to exec After hook", "After%s")
		}
	}()
	After%s(hookContext)
}

`, pascalName, hook.Function,
			pascalName,
			pascalName,
			pascalName))

		// go:linkname function declarations (link to external package)
		sb.WriteString(fmt.Sprintf("//go:linkname Before%s %s.Before%s\n", pascalName, hooksImportPath, pascalName))
		sb.WriteString(fmt.Sprintf("func Before%s(ctx hooks.HookContext)\n\n", pascalName))
		sb.WriteString(fmt.Sprintf("//go:linkname After%s %s.After%s\n", pascalName, hooksImportPath, pascalName))
		sb.WriteString(fmt.Sprintf("func After%s(ctx hooks.HookContext)\n\n", pascalName))
	}

	// Write to file
	return os.WriteFile(targetFile, []byte(sb.String()), 0644)
}

// instrumentFunction adds trampoline calls to the beginning and end of a function
// Uses the pattern: if hookContext, _ := OtelBeforeTrampoline_XXX(); false { } else { defer OtelAfterTrampoline_XXX(hookContext) }
func instrumentFunction(funcDecl *ast.FuncDecl, hook *HookDefinition) {
	if funcDecl.Body == nil {
		return
	}

	pascalName := capitalizeFirst(hook.Function)
	beforeTrampolineName := "OtelBeforeTrampoline_" + pascalName
	afterTrampolineName := "OtelAfterTrampoline_" + pascalName

	// Check if function is already instrumented by looking for existing trampoline calls
	for _, stmt := range funcDecl.Body.List {
		if ifStmt, ok := stmt.(*ast.IfStmt); ok {
			if assignStmt, ok := ifStmt.Init.(*ast.AssignStmt); ok {
				if callExpr, ok := assignStmt.Rhs[0].(*ast.CallExpr); ok {
					if ident, ok := callExpr.Fun.(*ast.Ident); ok && ident.Name == beforeTrampolineName {
						// Already instrumented, skip
						return
					}
				}
			}
		}
	}

	// Create the instrumentation pattern:
	// if hookContext, _ := OtelBeforeTrampoline_XXX(); false {
	// } else {
	//     defer OtelAfterTrampoline_XXX(hookContext)
	// }

	// The if statement with init
	instrumentStmt := &ast.IfStmt{
		Init: &ast.AssignStmt{
			Lhs: []ast.Expr{
				ast.NewIdent("hookContext" + pascalName),
				ast.NewIdent("_"),
			},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: ast.NewIdent(beforeTrampolineName),
				},
			},
		},
		Cond: ast.NewIdent("false"),
		Body: &ast.BlockStmt{
			List: []ast.Stmt{}, // Empty block for the "if false" branch
		},
		Else: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.DeferStmt{
					Call: &ast.CallExpr{
						Fun: ast.NewIdent(afterTrampolineName),
						Args: []ast.Expr{
							ast.NewIdent("hookContext" + pascalName),
						},
					},
				},
			},
		},
	}

	// Insert at the beginning of the function
	newBody := []ast.Stmt{instrumentStmt}
	newBody = append(newBody, funcDecl.Body.List...)
	funcDecl.Body.List = newBody
}

// capitalizeFirst capitalizes the first letter of a string
func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// stripTrimpath removes the -trimpath argument and its value from a compile command
// This preserves full WORK directory paths in the binary's debug info for dlv
// Pattern: -trimpath "$WORK/bXXX=>" or -trimpath $WORK/bXXX=>
func stripTrimpath(command string) string {
	// Pattern matches: -trimpath "$WORK/bXXX=>" or -trimpath '$WORK/bXXX=>' or -trimpath $WORK/bXXX=>
	// The value can be quoted with double quotes, single quotes, or unquoted
	re := regexp.MustCompile(`\s-trimpath\s+["']?\$WORK/b\d+=>['"']?`)
	return re.ReplaceAllString(command, " ")
}

// generateOtelRuntimeFile generates the otel.runtime.go file that imports the hooks package
// This file is added to the main package to ensure the hooks package is compiled and linked
func generateOtelRuntimeFile(targetDir string, hooksImportPath string) (string, error) {
	var sb strings.Builder

	sb.WriteString("// This file is generated by go-build-interceptor. DO NOT EDIT.\n")
	sb.WriteString("package main\n\n")
	sb.WriteString(fmt.Sprintf("import _ \"%s\" // Import hooks package to ensure it's compiled\n", hooksImportPath))

	targetFile := filepath.Join(targetDir, "otel.runtime.go")
	if err := os.WriteFile(targetFile, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write otel.runtime.go: %w", err)
	}

	return targetFile, nil
}

// generateHooksCompileCommand generates a compile command for the generated_hooks package
// Returns the compile commands (hooks lib + generated_hooks) and the output .a file path
func generateHooksCompileCommand(commands []Command, hooksFile string, hooksImportPath string, workDir string) (string, string) {
	// Find a sample compile command to extract the compiler path and common flags
	var sampleCmd string
	for _, cmd := range commands {
		if isCompileCommand(&cmd) {
			sampleCmd = cmd.Raw
			break
		}
	}
	if sampleCmd == "" {
		return "", ""
	}

	// Extract the compiler path from the sample command
	parts := strings.Fields(sampleCmd)
	if len(parts) < 1 {
		return "", ""
	}
	compilerPath := parts[0]

	// Get the hooks file directory and all .go files in it
	hooksDir := filepath.Dir(hooksFile)
	goFiles, err := filepath.Glob(filepath.Join(hooksDir, "*.go"))
	if err != nil || len(goFiles) == 0 {
		return "", ""
	}

	// Create output directory for hooks package
	hooksBuildDir := filepath.Join(workDir, "hooks_pkg")
	if err := os.MkdirAll(hooksBuildDir, 0755); err != nil {
		return "", ""
	}

	// Find the hooks library package (github.com/pdelewski/go-build-interceptor/hooks)
	hooksLibDir, hooksLibPkgFile, err := compileHooksLibrary(compilerPath, workDir, commands)
	if err != nil {
		fmt.Printf("           ⚠️  Failed to compile hooks library: %v\n", err)
		return "", ""
	}
	_ = hooksLibDir // suppress unused variable warning

	// Create importcfg for hooks package (including the hooks library)
	importcfgPath := filepath.Join(hooksBuildDir, "importcfg")
	if err := createHooksImportcfg(importcfgPath, commands, workDir, hooksLibPkgFile); err != nil {
		fmt.Printf("           ⚠️  Failed to create hooks importcfg: %v\n", err)
		return "", ""
	}

	// Output file path
	outputFile := filepath.Join(hooksBuildDir, "_pkg_.a")

	// Build the compile command
	var sb strings.Builder
	sb.WriteString(compilerPath)
	sb.WriteString(" -o ")
	sb.WriteString(outputFile)
	sb.WriteString(" -p ")
	sb.WriteString(hooksImportPath)
	sb.WriteString(" -importcfg ")
	sb.WriteString(importcfgPath)
	sb.WriteString(" -pack")

	// Add all .go files
	for _, goFile := range goFiles {
		// Skip test files
		if strings.HasSuffix(goFile, "_test.go") {
			continue
		}
		sb.WriteString(" ")
		sb.WriteString(goFile)
	}

	return sb.String(), outputFile
}

// compileHooksLibrary compiles the github.com/pdelewski/go-build-interceptor/hooks package (types.go only)
func compileHooksLibrary(compilerPath string, workDir string, commands []Command) (string, string, error) {
	// Find the hooks library directory
	// First try using the executable path to find the module
	execPath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("failed to get executable path: %w", err)
	}
	moduleDir := filepath.Dir(execPath)
	hooksLibDir := filepath.Join(moduleDir, "hooks")

	// Check if the hooks directory exists
	if _, err := os.Stat(hooksLibDir); os.IsNotExist(err) {
		// Try go list as fallback (run from the module directory if possible)
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/pdelewski/go-build-interceptor")
		cmd.Dir = moduleDir // Run from the module directory
		output, err := cmd.Output()
		if err != nil {
			// Last resort: look in parent directories
			for dir := moduleDir; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
				testPath := filepath.Join(dir, "hooks")
				if _, err := os.Stat(testPath); err == nil {
					hooksLibDir = testPath
					break
				}
			}
			// Check if we found it
			if _, err := os.Stat(hooksLibDir); os.IsNotExist(err) {
				return "", "", fmt.Errorf("hooks library not found (tried %s)", hooksLibDir)
			}
		} else {
			moduleDir = strings.TrimSpace(string(output))
			hooksLibDir = filepath.Join(moduleDir, "hooks")
		}
	}

	// Only compile types.go (lightweight, no dependencies)
	// hooks.go has heavy dependencies (context, go/ast) that we don't need
	typesFile := filepath.Join(hooksLibDir, "types.go")
	if _, err := os.Stat(typesFile); os.IsNotExist(err) {
		return "", "", fmt.Errorf("types.go not found in hooks library: %s", hooksLibDir)
	}

	// Create output directory
	hooksLibBuildDir := filepath.Join(workDir, "hooks_lib")
	if err := os.MkdirAll(hooksLibBuildDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create hooks lib build dir: %w", err)
	}

	// Create importcfg for hooks library (no dependencies needed - types.go is self-contained)
	importcfgPath := filepath.Join(hooksLibBuildDir, "importcfg")
	if err := os.WriteFile(importcfgPath, []byte("# import config\n"), 0644); err != nil {
		return "", "", fmt.Errorf("failed to create hooks lib importcfg: %w", err)
	}

	// Output file path
	outputFile := filepath.Join(hooksLibBuildDir, "_pkg_.a")

	// Build the compile command - only compile types.go
	var sb strings.Builder
	sb.WriteString(compilerPath)
	sb.WriteString(" -o ")
	sb.WriteString(outputFile)
	sb.WriteString(" -p github.com/pdelewski/go-build-interceptor/hooks")
	sb.WriteString(" -importcfg ")
	sb.WriteString(importcfgPath)
	sb.WriteString(" -pack ")
	sb.WriteString(typesFile)

	// Execute the compile command
	compileCmd := sb.String()
	fmt.Printf("           📦 Compiling hooks library (types.go)...\n")
	execCmd := exec.Command("bash", "-c", compileCmd)
	execCmd.Dir = hooksLibDir
	if output, err := execCmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("failed to compile hooks library: %w\nOutput: %s", err, string(output))
	}

	return hooksLibDir, outputFile, nil
}

// createMinimalImportcfg creates an importcfg with minimal dependencies
func createMinimalImportcfg(path string, commands []Command, workDir string) error {
	// Find commonly used packages from existing compile commands
	packagePaths := make(map[string]string)

	for _, cmd := range commands {
		if !isCompileCommand(&cmd) {
			continue
		}

		parts := strings.Fields(cmd.Raw)
		var outputFile, pkgName string
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "-o" {
				outputFile = parts[i+1]
				outputFile = strings.ReplaceAll(outputFile, "$WORK", workDir)
			}
			if parts[i] == "-p" {
				pkgName = parts[i+1]
			}
		}

		if outputFile != "" && pkgName != "" {
			packagePaths[pkgName] = outputFile
		}
	}

	var sb strings.Builder
	sb.WriteString("# import config\n")
	for pkgName, pkgPath := range packagePaths {
		sb.WriteString(fmt.Sprintf("packagefile %s=%s\n", pkgName, pkgPath))
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// createHooksImportcfg creates an importcfg file for the generated_hooks package
func createHooksImportcfg(path string, commands []Command, workDir string, hooksLibPkgFile string) error {
	// Find commonly used packages from existing compile commands
	packagePaths := make(map[string]string)

	for _, cmd := range commands {
		if !isCompileCommand(&cmd) {
			continue
		}

		// Extract -o (output file) and -p (package name)
		parts := strings.Fields(cmd.Raw)
		var outputFile, pkgName string
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "-o" {
				outputFile = parts[i+1]
				// Resolve $WORK to actual path
				outputFile = strings.ReplaceAll(outputFile, "$WORK", workDir)
			}
			if parts[i] == "-p" {
				pkgName = parts[i+1]
			}
		}

		if outputFile != "" && pkgName != "" {
			packagePaths[pkgName] = outputFile
		}
	}

	// Write importcfg
	var sb strings.Builder
	sb.WriteString("# import config\n")

	// Add the hooks library package
	if hooksLibPkgFile != "" {
		sb.WriteString(fmt.Sprintf("packagefile github.com/pdelewski/go-build-interceptor/hooks=%s\n", hooksLibPkgFile))
	}

	// Add all packages (the hooks package may need various dependencies)
	for pkgName, pkgPath := range packagePaths {
		sb.WriteString(fmt.Sprintf("packagefile %s=%s\n", pkgName, pkgPath))
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// updateMainImportcfg updates the main package's importcfg to include the hooks package
func updateMainImportcfg(compileCmd string, hooksImportPath string, hooksPkgFile string) error {
	// Find -importcfg in the compile command
	parts := strings.Fields(compileCmd)
	var importcfgPath string
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "-importcfg" {
			importcfgPath = parts[i+1]
			break
		}
	}

	if importcfgPath == "" {
		return fmt.Errorf("importcfg not found in compile command")
	}

	// Read existing importcfg
	content, err := os.ReadFile(importcfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg: %w", err)
	}

	// Add the hooks package
	newLine := fmt.Sprintf("packagefile %s=%s\n", hooksImportPath, hooksPkgFile)

	// Check if already present
	if strings.Contains(string(content), hooksImportPath) {
		return nil
	}

	// Append to importcfg
	newContent := string(content) + newLine
	if err := os.WriteFile(importcfgPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write importcfg: %w", err)
	}

	fmt.Printf("           📎 Updated importcfg to include hooks package: %s\n", hooksImportPath)
	return nil
}

// copyAndInstrumentFileOnly copies and instruments a source file without replacing the original
func copyAndInstrumentFileOnly(sourceFile string, workDir string, buildID string, packageName string, hooks []HookDefinition, hooksImportPath string) error {
	if workDir == "" || buildID == "" {
		return fmt.Errorf("missing work directory or build ID")
	}

	// Create the target directory: $WORK/buildID/
	targetDir := filepath.Join(workDir, buildID)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	// Determine target file path
	sourceBaseName := filepath.Base(sourceFile)
	targetFile := filepath.Join(targetDir, sourceBaseName)

	// Instrument the file instead of just copying
	if err := instrumentFile(sourceFile, targetFile, packageName, hooks, hooksImportPath); err != nil {
		return fmt.Errorf("failed to instrument file: %w", err)
	}

	fmt.Printf("           📄 Copied and instrumented %s to %s\n", sourceBaseName, targetFile)
	return nil
}

// generateModifiedBuildLog generates a new build log with updated file paths for instrumented files
func generateModifiedBuildLog(commands []Command, fileReplacements map[string]string, trampolineFiles map[string]string, generatedFilePaths map[string][]string, hooksImportPath string, workDir string, hooksFile string, otelRuntimeFile string, mainPackageInfo *PackagePathInfo) error {
	outputFile := "go-build-modified.log"

	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create modified build log: %w", err)
	}
	defer file.Close()

	// Generate compile command for generated_hooks package
	hooksCompileCmd := ""
	hooksPkgFile := ""
	if hooksFile != "" && workDir != "" {
		hooksCompileCmd, hooksPkgFile = generateHooksCompileCommand(commands, hooksFile, hooksImportPath, workDir)
		if hooksCompileCmd != "" {
			fmt.Printf("📦 Generated compile command for hooks package\n")
		}
	}

	// Determine the main package's buildID (usually b001)
	mainBuildID := ""
	if mainPackageInfo != nil {
		mainBuildID = mainPackageInfo.BuildID
	}

	// Track if we've inserted the hooks compile command
	hooksCompileInserted := false

	for _, cmd := range commands {
		modifiedCommand := cmd.Raw

		// Check if this is an importcfg heredoc for main package
		if cmd.IsMultiline && mainBuildID != "" && hooksPkgFile != "" {
			// Check if this heredoc creates the main package's importcfg (compile or link)
			if strings.Contains(modifiedCommand, "/"+mainBuildID+"/importcfg") &&
				strings.Contains(modifiedCommand, "<< 'EOF'") {
				// Inject the hooks packages before EOF
				hooksPackageLine := fmt.Sprintf("packagefile %s=%s", hooksImportPath, hooksPkgFile)
				hooksLibPkgFile := filepath.Join(workDir, "hooks_lib", "_pkg_.a")
				hooksLibPackageLine := fmt.Sprintf("packagefile github.com/pdelewski/go-build-interceptor/hooks=%s", hooksLibPkgFile)

				// Check if this is the link importcfg or compile importcfg
				if strings.Contains(modifiedCommand, "importcfg.link") {
					// For link, add both generated_hooks and hooks library
					modifiedCommand = strings.Replace(modifiedCommand, "\nEOF\n", "\n"+hooksPackageLine+"\n"+hooksLibPackageLine+"\nEOF\n", 1)
					fmt.Printf("           📎 Added packages to main importcfg.link heredoc\n")
				} else {
					// For compile, add both generated_hooks and hooks library (trampolines import hooks)
					modifiedCommand = strings.Replace(modifiedCommand, "\nEOF\n", "\n"+hooksPackageLine+"\n"+hooksLibPackageLine+"\nEOF\n", 1)
					fmt.Printf("           📎 Added packages to main importcfg heredoc\n")
				}
			}
		}

		// If this is a compile command, check if we need to replace any file paths
		if isCompileCommand(&cmd) {
			packageName := extractPackageName(&cmd)
			needsTrampolineFile := false

			// Insert hooks compile command before main package
			if packageName == "main" && hooksCompileCmd != "" && !hooksCompileInserted {
				if _, err := fmt.Fprintf(file, "%s\n", hooksCompileCmd); err != nil {
					return fmt.Errorf("failed to write hooks compile command: %w", err)
				}
				hooksCompileInserted = true
				fmt.Printf("           📎 Inserted hooks compile command before main\n")
			}

			// Check if this package has instrumented files
			hasInstrumentedFiles := false
			for originalFile := range fileReplacements {
				if strings.Contains(modifiedCommand, originalFile) || strings.Contains(modifiedCommand, filepath.Base(originalFile)) {
					hasInstrumentedFiles = true
					break
				}
			}

			// Strip -trimpath for instrumented packages so dlv can find source files
			// This preserves full WORK directory paths in the binary's debug info
			if hasInstrumentedFiles {
				modifiedCommand = stripTrimpath(modifiedCommand)
				fmt.Printf("           🔧 Stripped -trimpath for package '%s' to preserve debug paths\n", packageName)
			}

			// Replace file paths in the command - but only for Go files
			for originalFile, instrumentedFile := range fileReplacements {
				// Only replace if the original file is a .go file
				if !strings.HasSuffix(originalFile, ".go") {
					continue
				}

				// Check if this replacement is for the current package
				if strings.Contains(modifiedCommand, originalFile) {
					needsTrampolineFile = true
				}

				// Replace both absolute and relative paths
				modifiedCommand = strings.ReplaceAll(modifiedCommand, originalFile, instrumentedFile)

				// Also try replacing just the filename in case the command uses relative paths
				originalBasename := filepath.Base(originalFile)
				instrumentedBasename := filepath.Base(instrumentedFile)
				if originalBasename != instrumentedBasename {
					modifiedCommand = strings.ReplaceAll(modifiedCommand, originalBasename, instrumentedFile)
				} else {
					// If basenames are the same, we need to replace the full path context
					// Look for the file in -pack arguments
					modifiedCommand = strings.ReplaceAll(modifiedCommand, " "+originalBasename+" ", " "+instrumentedFile+" ")
					modifiedCommand = strings.ReplaceAll(modifiedCommand, " "+originalBasename+"$", " "+instrumentedFile)
				}
			}

			// Add trampolines file to the compile command if this package has hooks
			if needsTrampolineFile {
				if trampolinesFile, exists := trampolineFiles[packageName]; exists {
					// Append the trampolines file at the end of the compile command
					modifiedCommand = modifiedCommand + " " + trampolinesFile
					fmt.Printf("           📎 Adding trampolines file to compile command for package '%s': %s\n", packageName, trampolinesFile)

					// Strip -complete flag as we have functions without body (go:linkname declarations)
					modifiedCommand = strings.Replace(modifiedCommand, " -complete ", " ", 1)
				}
			}

			// Add generated files to compile command if this package has any
			if genFiles, exists := generatedFilePaths[packageName]; exists && len(genFiles) > 0 {
				for _, genFile := range genFiles {
					modifiedCommand = modifiedCommand + " " + genFile
					fmt.Printf("           📎 Adding generated file to compile command for package '%s': %s\n", packageName, filepath.Base(genFile))
				}
				// Strip -complete flag as we're adding generated files
				modifiedCommand = strings.Replace(modifiedCommand, " -complete ", " ", 1)
			}

			// Add otel.runtime.go to main package compile command
			if packageName == "main" && otelRuntimeFile != "" {
				modifiedCommand = modifiedCommand + " " + otelRuntimeFile
				fmt.Printf("           📎 Adding otel.runtime.go to main package compile\n")

				// Strip -complete flag for main as well (otel.runtime.go might have import issues during initial compile)
				modifiedCommand = strings.Replace(modifiedCommand, " -complete ", " ", 1)
			}
		}

		// Write the (potentially modified) command to the new log file
		if _, err := fmt.Fprintf(file, "%s\n", modifiedCommand); err != nil {
			return fmt.Errorf("failed to write command to modified build log: %w", err)
		}
	}

	return nil
}

// executeModifiedBuildLogWithParser executes the modified build log using the existing Parser functionality
func executeModifiedBuildLogWithParser(logFile string) error {
	// Create a new parser and parse the modified log file
	modifiedParser := NewParser()
	if err := modifiedParser.ParseFile(logFile); err != nil {
		return fmt.Errorf("failed to parse modified log file: %w", err)
	}

	// Generate the script but don't execute it yet
	if err := modifiedParser.GenerateScript(); err != nil {
		return fmt.Errorf("failed to generate script from modified log file: %w", err)
	}

	// Now execute the script with proper error handling
	fmt.Printf("Generated script from modified build log. Running replay_script.sh...\n")
	if err := modifiedParser.ExecuteScript(); err != nil {
		return fmt.Errorf("failed to execute modified build script: %w", err)
	}

	return nil
}
