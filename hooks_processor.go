package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
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
	packagesWithMatches := make(map[string]bool) // Track packages that have matches
	copiedFiles := make(map[string]bool)         // Track files already copied per package
	fileReplacements := make(map[string]string)  // Track original file -> instrumented file mapping

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
						if err := copyAndInstrumentFileOnly(file, workDir, pkgInfo.BuildID, packageName, hooks); err != nil {
							fmt.Printf("           ⚠️  Failed to copy and instrument file: %v\n", err)
						} else {
							copiedFiles[copyKey] = true
							// Track the file replacement mapping - only for Go files
							if strings.HasSuffix(file, ".go") {
								fileReplacements[file] = instrumentedFilePath
								fmt.Printf("           🔄 Will replace %s with %s in compile command\n", file, instrumentedFilePath)
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
		if err := generateModifiedBuildLog(commands, fileReplacements); err != nil {
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

// copyAndInstrumentFile copies and instruments a source file to the work directory for a package
func copyAndInstrumentFile(sourceFile string, workDir string, buildID string, packageName string, hooks []HookDefinition) error {
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
	if err := instrumentFile(sourceFile, targetFile, packageName, hooks); err != nil {
		return fmt.Errorf("failed to instrument file: %w", err)
	}

	fmt.Printf("           📄 Copied and instrumented %s to %s\n", sourceBaseName, targetFile)

	// Replace the original source file with the instrumented version
	if err := replaceOriginalWithInstrumented(sourceFile, targetFile); err != nil {
		fmt.Printf("           ⚠️  Failed to replace original file: %v\n", err)
	} else {
		fmt.Printf("           🔄 Replaced original %s with instrumented version\n", sourceBaseName)
	}

	return nil
}

// copyFileToWorkDir copies a source file to the work directory for a package
func copyFileToWorkDir(sourceFile string, workDir string, buildID string) error {
	if workDir == "" || buildID == "" {
		return fmt.Errorf("missing work directory or build ID")
	}

	// Create the target directory: $WORK/buildID/src/
	targetDir := filepath.Join(workDir, buildID, "src")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	// Copy the source file to the target directory
	sourceBaseName := filepath.Base(sourceFile)
	targetFile := filepath.Join(targetDir, sourceBaseName)

	// Open source file
	src, err := os.Open(sourceFile)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", sourceFile, err)
	}
	defer src.Close()

	// Create target file
	dst, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("failed to create target file %s: %w", targetFile, err)
	}
	defer dst.Close()

	// Copy content
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	fmt.Printf("           📄 Copied %s to %s\n", sourceBaseName, targetFile)
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
func instrumentFile(sourceFile, targetFile string, packageName string, hooks []HookDefinition) error {
	// Parse the source file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse source file %s: %w", sourceFile, err)
	}

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

	// Add trampoline function definitions if we have applicable hooks
	if len(applicableHooks) > 0 {
		addTrampolineFunctions(node, applicableHooks)
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

	if len(instrumentedFunctions) > 0 {
		fmt.Printf("           🔧 Instrumented functions: %s\n", strings.Join(instrumentedFunctions, ", "))
	}

	return nil
}

// addTrampolineFunctions adds empty trampoline function definitions to the AST
func addTrampolineFunctions(node *ast.File, hooks []HookDefinition) {
	// Track which trampoline functions have already been added
	existingTrampolines := make(map[string]bool)

	// Check for existing trampoline functions
	for _, decl := range node.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			if strings.HasPrefix(funcDecl.Name.Name, "trampoline_") {
				existingTrampolines[funcDecl.Name.Name] = true
			}
		}
	}

	for _, hook := range hooks {
		if hook.Type == "before_after" || hook.Type == "both" {
			beforeFuncName := "trampoline_Before" + capitalizeFirst(hook.Function)
			afterFuncName := "trampoline_After" + capitalizeFirst(hook.Function)

			// Only add if not already present
			if !existingTrampolines[beforeFuncName] {
				// Create trampoline_BeforeXXX function
				beforeFunc := &ast.FuncDecl{
					Name: ast.NewIdent(beforeFuncName),
					Type: &ast.FuncType{
						Params:  &ast.FieldList{},
						Results: nil,
					},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							&ast.ExprStmt{
								X: &ast.CallExpr{
									Fun: &ast.SelectorExpr{
										X:   ast.NewIdent("fmt"),
										Sel: ast.NewIdent("Println"),
									},
									Args: []ast.Expr{
										&ast.BasicLit{
											Kind:  token.STRING,
											Value: fmt.Sprintf("\"[TRAMPOLINE] Before %s\"", hook.Function),
										},
									},
								},
							},
						},
					},
				}
				node.Decls = append(node.Decls, beforeFunc)
				existingTrampolines[beforeFuncName] = true
			}

			// Only add if not already present
			if !existingTrampolines[afterFuncName] {
				// Create trampoline_AfterXXX function
				afterFunc := &ast.FuncDecl{
					Name: ast.NewIdent(afterFuncName),
					Type: &ast.FuncType{
						Params:  &ast.FieldList{},
						Results: nil,
					},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							&ast.ExprStmt{
								X: &ast.CallExpr{
									Fun: &ast.SelectorExpr{
										X:   ast.NewIdent("fmt"),
										Sel: ast.NewIdent("Println"),
									},
									Args: []ast.Expr{
										&ast.BasicLit{
											Kind:  token.STRING,
											Value: fmt.Sprintf("\"[TRAMPOLINE] After %s\"", hook.Function),
										},
									},
								},
							},
						},
					},
				}
				node.Decls = append(node.Decls, afterFunc)
				existingTrampolines[afterFuncName] = true
			}
		}
	}
}

// instrumentFunction adds trampoline calls to the beginning and end of a function
func instrumentFunction(funcDecl *ast.FuncDecl, hook *HookDefinition) {
	if funcDecl.Body == nil {
		return
	}

	// Check if function is already instrumented by looking for existing trampoline calls
	trampolineName := "trampoline_Before" + capitalizeFirst(hook.Function)
	for _, stmt := range funcDecl.Body.List {
		if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
			if callExpr, ok := exprStmt.X.(*ast.CallExpr); ok {
				if ident, ok := callExpr.Fun.(*ast.Ident); ok && ident.Name == trampolineName {
					// Already instrumented, skip
					return
				}
			}
		}
	}

	// Create before call
	beforeCall := &ast.ExprStmt{
		X: &ast.CallExpr{
			Fun: ast.NewIdent("trampoline_Before" + capitalizeFirst(hook.Function)),
		},
	}

	// Create after call (using defer to ensure it runs even if function panics)
	afterCall := &ast.DeferStmt{
		Call: &ast.CallExpr{
			Fun: ast.NewIdent("trampoline_After" + capitalizeFirst(hook.Function)),
		},
	}

	// Insert at the beginning of the function
	newBody := []ast.Stmt{beforeCall, afterCall}
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

// replaceOriginalWithInstrumented replaces the original source file with the instrumented version
func replaceOriginalWithInstrumented(originalFile, instrumentedFile string) error {
	// First, backup the original file
	backupFile := originalFile + ".backup"
	if err := copyFile(originalFile, backupFile); err != nil {
		return fmt.Errorf("failed to backup original file: %w", err)
	}

	// Replace the original with the instrumented version
	if err := copyFile(instrumentedFile, originalFile); err != nil {
		// If replacement fails, restore from backup
		copyFile(backupFile, originalFile)
		return fmt.Errorf("failed to replace original file: %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

// copyAndInstrumentFileOnly copies and instruments a source file without replacing the original
func copyAndInstrumentFileOnly(sourceFile string, workDir string, buildID string, packageName string, hooks []HookDefinition) error {
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
	if err := instrumentFile(sourceFile, targetFile, packageName, hooks); err != nil {
		return fmt.Errorf("failed to instrument file: %w", err)
	}

	fmt.Printf("           📄 Copied and instrumented %s to %s\n", sourceBaseName, targetFile)
	return nil
}

// generateModifiedBuildLog generates a new build log with updated file paths for instrumented files
func generateModifiedBuildLog(commands []Command, fileReplacements map[string]string) error {
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
			// Replace file paths in the command - but only for Go files
			for originalFile, instrumentedFile := range fileReplacements {
				// Only replace if the original file is a .go file
				if !strings.HasSuffix(originalFile, ".go") {
					continue
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
