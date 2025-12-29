package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// HookDefinition represents a parsed hook from the hooks file
type HookDefinition struct {
	Package  string
	Function string
	Receiver string
	Type     string // "before_after", "rewrite", or "both"
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
		return fmt.Errorf("error parsing hooks file: %w", err)
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
	packagesWithMatches := make(map[string]bool)   // Track packages that have matches
	copiedFiles := make(map[string]bool)           // Track files already copied per package
	fileReplacements := make(map[string]string)    // Track original file -> instrumented file mapping
	trampolineFiles := make(map[string]string)     // Track package -> trampolines file path

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
					case "rewrite":
						fmt.Printf("           Will rewrite: Function signature and body\n")
					case "both":
						fmt.Printf("           Will inject: Before/After hooks AND rewrite function\n")
					}
				}
			}

			// Copy and instrument the source file to work directory if it has matches and hasn't been copied yet
			if fileHasMatches && workDir != "" {
				copyKey := packageName + ":" + file
				if !copiedFiles[copyKey] {
					if pkgInfo, exists := packageInfo[packageName]; exists && pkgInfo.BuildID != "" {
						instrumentedFilePath := filepath.Join(workDir, pkgInfo.BuildID, "src", filepath.Base(file))
						if err := copyAndInstrumentFileOnly(file, workDir, pkgInfo.BuildID, packageName, hooks, hooksImportPath); err != nil {
							fmt.Printf("           ⚠️  Failed to copy and instrument file: %v\n", err)
						} else {
							copiedFiles[copyKey] = true
							// Track the file replacement mapping - only for Go files
							if strings.HasSuffix(file, ".go") {
								fileReplacements[file] = instrumentedFilePath
								fmt.Printf("           🔄 Will replace %s with %s in compile command\n", file, instrumentedFilePath)

								// Track the trampolines file for this package
								trampolinesPath := filepath.Join(workDir, pkgInfo.BuildID, "src", "otel_trampolines.go")
								trampolineFiles[packageName] = trampolinesPath
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
	}

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

	// Generate modified build log with updated file paths
	if len(fileReplacements) > 0 {
		if err := generateModifiedBuildLog(commands, fileReplacements, trampolineFiles); err != nil {
			fmt.Printf("⚠️  Failed to generate modified build log: %v\n", err)
		} else {
			fmt.Printf("\n📄 Generated modified build log: go-build-modified.log\n")

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
				if match.Type == "before_after" || match.Type == "both" {
					applicableHooks = append(applicableHooks, *match)
					instrumentedFunctions = append(instrumentedFunctions, funcDecl.Name.Name)

					// Instrument the function
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

	return nil
}

// generateTrampolinesFile creates a separate file with trampoline functions and go:linkname declarations
func generateTrampolinesFile(targetFile string, packageName string, hooks []HookDefinition, hooksImportPath string) error {
	var sb strings.Builder

	// Write package declaration
	sb.WriteString(fmt.Sprintf("package %s\n\n", packageName))

	// Write imports
	sb.WriteString("import _ \"unsafe\" // Required for go:linkname\n\n")

	fmt.Printf("           🔗 Using hooks import path for go:linkname: %s\n", hooksImportPath)

	// Write HookContext interface (only once)
	sb.WriteString(`// HookContext provides a minimal interface for hook functions
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

`)

	// Generate trampolines for each hook
	for _, hook := range hooks {
		pascalName := capitalizeFirst(hook.Function)

		// HookContextImpl struct
		sb.WriteString(fmt.Sprintf(`// HookContextImpl%s implements HookContext for %s
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

		// Before trampoline
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
	if Before%s != nil {
		Before%s(hookContext)
	}
	return hookContext, hookContext.skipCall
}

`, pascalName, hook.Function,
			pascalName, pascalName,
			pascalName,
			pascalName,
			hook.Function, hook.Package,
			pascalName, pascalName))

		// After trampoline
		sb.WriteString(fmt.Sprintf(`// OtelAfterTrampoline_%s is the after trampoline for %s
func OtelAfterTrampoline_%s(hookContext HookContext) {
	defer func() {
		if err := recover(); err != nil {
			println("failed to exec After hook", "After%s")
		}
	}()
	if After%s != nil {
		After%s(hookContext)
	}
}

`, pascalName, hook.Function,
			pascalName,
			pascalName,
			pascalName, pascalName))

		// go:linkname declarations (note: no space between // and go)
		// Use full import path for go:linkname to link to the hooks package
		sb.WriteString(fmt.Sprintf("//go:linkname Before%s %s.Before%s\n", pascalName, hooksImportPath, pascalName))
		sb.WriteString(fmt.Sprintf("var Before%s func(ctx HookContext)\n\n", pascalName))
		sb.WriteString(fmt.Sprintf("//go:linkname After%s %s.After%s\n", pascalName, hooksImportPath, pascalName))
		sb.WriteString(fmt.Sprintf("var After%s func(ctx HookContext)\n\n", pascalName))
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

// addBlankImport adds a blank import (import _ "path") to a Go AST
func addBlankImport(node *ast.File, importPath string) {
	// Check if import already exists
	for _, imp := range node.Imports {
		if imp.Path != nil && strings.Trim(imp.Path.Value, `"`) == importPath {
			return // Import already exists
		}
	}

	// Create the new import spec: _ "importPath"
	newImport := &ast.ImportSpec{
		Name: ast.NewIdent("_"),
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: fmt.Sprintf(`"%s"`, importPath),
		},
	}

	// Find or create import declaration
	var importDecl *ast.GenDecl
	for _, decl := range node.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			importDecl = genDecl
			break
		}
	}

	if importDecl != nil {
		// Add to existing import declaration
		importDecl.Specs = append(importDecl.Specs, newImport)
	} else {
		// Create new import declaration
		importDecl = &ast.GenDecl{
			Tok:   token.IMPORT,
			Specs: []ast.Spec{newImport},
		}
		// Insert after package declaration
		newDecls := []ast.Decl{importDecl}
		newDecls = append(newDecls, node.Decls...)
		node.Decls = newDecls
	}

	// Update the Imports slice
	node.Imports = append(node.Imports, newImport)
}

// copyAndInstrumentFileOnly copies and instruments a source file without replacing the original
func copyAndInstrumentFileOnly(sourceFile string, workDir string, buildID string, packageName string, hooks []HookDefinition, hooksImportPath string) error {
	if workDir == "" || buildID == "" {
		return fmt.Errorf("missing work directory or build ID")
	}

	// Create the target directory: $WORK/buildID/src/
	targetDir := filepath.Join(workDir, buildID, "src")
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
func generateModifiedBuildLog(commands []Command, fileReplacements map[string]string, trampolineFiles map[string]string) error {
	outputFile := "go-build-modified.log"

	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create modified build log: %w", err)
	}
	defer file.Close()

	for _, cmd := range commands {
		modifiedCommand := cmd.Raw

		// If this is a compile command, check if we need to replace any file paths
		if isCompileCommand(&cmd) {
			packageName := extractPackageName(&cmd)
			needsTrampolineFile := false

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
				}
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
