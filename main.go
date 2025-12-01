package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"strings"
)

func main() {
	// Parse command line flags
	config := ParseFlags()

	// Create processor
	processor := NewProcessor(config)

	// Run the processor
	if err := processor.Run(); err != nil {
		log.Fatalf("Error during execution: %v", err)
	}
}

// Processor handles the main processing logic
type Processor struct {
	config *Config
	parser *Parser
}

// NewProcessor creates a new processor with the given config
func NewProcessor(config *Config) *Processor {
	return &Processor{
		config: config,
		parser: NewParser(),
	}
}

// Run executes the main processing flow
func (p *Processor) Run() error {
	mode := p.config.GetExecutionMode()

	// Capture modes don't need to parse log file
	if mode != "capture" && mode != "json-capture" {
		// Parse the log file
		if err := p.parser.ParseFile(p.config.LogFile); err != nil {
			return fmt.Errorf("error parsing file: %w", err)
		}

		commands := p.parser.GetCommands()
		fmt.Printf("Parsed %d commands from %s\n\n", len(commands), p.config.LogFile)
	}

	// Set up WORK environment if needed
	if err := p.setupWorkEnvironment(); err != nil {
		return err
	}

	// Execute based on mode
	return p.executeMode()
}

// setupWorkEnvironment creates a temp work directory if needed
func (p *Processor) setupWorkEnvironment() error {
	mode := p.config.GetExecutionMode()
	if os.Getenv("WORK") == "" && (mode == "interactive" || mode == "execute") {
		tmpDir, err := os.MkdirTemp("", "go-build-replay")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		os.Setenv("WORK", tmpDir)
		fmt.Printf("Created WORK directory: %s\n\n", tmpDir)

		// Note: We don't defer cleanup here since the commands might need the directory
		// The directory will be cleaned up when the program exits
	}
	return nil
}

// executeMode executes the appropriate mode based on config
func (p *Processor) executeMode() error {
	mode := p.config.GetExecutionMode()
	commands := p.parser.GetCommands()

	switch mode {
	case "capture":
		fmt.Println("=== Capture Mode ===")
		capturer := &TextCapturer{}
		if err := capturer.Capture(); err != nil {
			return fmt.Errorf("capture failed: %w", err)
		}
		fmt.Println(capturer.GetDescription())
	case "json-capture":
		fmt.Println("=== JSON Capture Mode ===")
		capturer := &JSONCapturer{}
		if err := capturer.Capture(); err != nil {
			return fmt.Errorf("JSON capture failed: %w", err)
		}
		fmt.Println(capturer.GetDescription())
	case "pack-functions":
		fmt.Println("=== Pack Functions Mode ===")
		compileCount := 0
		totalFuncs := 0

		for _, cmd := range commands {
			if isCompileCommand(&cmd) {
				compileCount++
				files := extractPackFiles(&cmd)
				for _, file := range files {
					// Only process .go files
					if strings.HasSuffix(file, ".go") {
						functions, err := extractFunctionsFromGoFile(file)
						if err != nil {
							fmt.Printf("  Error parsing %s: %v\n", file, err)
							continue
						}
						if len(functions) > 0 {
							fmt.Printf("\nFile: %s\n", file)
							for _, fn := range functions {
								// Build function signature
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

								fmt.Printf("  - %s", sig.String())
								if fn.IsExported {
									fmt.Print(" [exported]")
								}
								fmt.Println()
								totalFuncs++
							}
						}
					}
				}
			}
		}

		if compileCount > 0 {
			fmt.Printf("\nProcessed %d compile commands, found %d functions/methods.\n", compileCount, totalFuncs)
		} else {
			fmt.Println("No compile commands found.")
		}
	case "pack-files":
		fmt.Println("=== Pack Files Mode ===")
		compileCount := 0
		totalFiles := 0

		for _, cmd := range commands {
			if isCompileCommand(&cmd) {
				compileCount++
				files := extractPackFiles(&cmd)
				if len(files) > 0 {
					totalFiles += len(files)
					fmt.Printf("Compile command %d: Found %d files after -pack flag:\n", compileCount, len(files))

					// Process each file with a custom action
					processPackFiles(files, func(file string) {
						fmt.Printf("  - %s\n", file)
						// Add your custom action here for each file
						// For example: analyzeFile(file), transformFile(file), etc.
					})
					fmt.Println()
				}
			}
		}

		if compileCount > 0 {
			fmt.Printf("Processed %d compile commands with %d total files.\n", compileCount, totalFiles)
		} else {
			fmt.Println("No compile commands found.")
		}
	case "verbose":
		p.parser.DumpCommands()
	case "dump":
		for i, cmd := range commands {
			fmt.Printf("# Command %d\n", i+1)
			fmt.Println(cmd.String())
		}
	case "dry-run":
		fmt.Println("=== Dry Run Mode ===")
		for i, cmd := range commands {
			if cmd.Executable == "" {
				continue
			}
			fmt.Printf("Command %d: %s\n", i+1, cmd.String())
		}
	case "interactive":
		if err := p.parser.ExecuteInteractive(); err != nil {
			log.Printf("Error in interactive mode: %v", err)
		}
	case "execute":
		fmt.Println("=== Generating and Executing Script ===")
		if err := p.parser.ExecuteAll(); err != nil {
			log.Printf("Error executing commands: %v", err)
		} else {
			fmt.Println("\nReplay completed successfully!")
		}
	default: // "generate"
		fmt.Println("=== Generating Script ===")
		if err := p.parser.GenerateScript(); err != nil {
			log.Printf("Error generating script: %v", err)
		} else {
			fmt.Println("\nScript generated successfully! Use --execute flag to run it.")
		}
	}

	return nil
}

// isCompileCommand checks if a command is a compile command
func isCompileCommand(cmd *Command) bool {
	return cmd.Executable != "" && strings.HasSuffix(cmd.Executable, "/compile")
}

// extractPackFiles extracts files listed after the -pack flag in a compile command
func extractPackFiles(cmd *Command) []string {
	var files []string
	packIndex := -1

	// Find the -pack flag
	for i, arg := range cmd.Args {
		if arg == "-pack" {
			packIndex = i
			break
		}
	}

	// If -pack flag found, collect all remaining arguments as files
	if packIndex >= 0 && packIndex+1 < len(cmd.Args) {
		files = cmd.Args[packIndex+1:]
	}

	return files
}

// processPackFiles processes the pack files with a custom action
func processPackFiles(files []string, action func(string)) {
	for _, file := range files {
		action(file)
	}
}

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
