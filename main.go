package main

import (
	"fmt"
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
	case "pack-packages":
		fmt.Println("=== Pack Packages Mode ===")
		compileCount := 0
		packageNames := make(map[string]int)

		for _, cmd := range commands {
			if isCompileCommand(&cmd) {
				compileCount++
				packageName := extractPackageName(&cmd)
				if packageName != "" {
					packageNames[packageName]++
				}
			}
		}

		if len(packageNames) > 0 {
			fmt.Printf("Found %d unique packages in %d compile commands:\n\n", len(packageNames), compileCount)
			for pkg, count := range packageNames {
				fmt.Printf("  - %s", pkg)
				if count > 1 {
					fmt.Printf(" (compiled %d times)", count)
				}
				fmt.Println()
			}
		} else {
			fmt.Println("No package names found in compile commands.")
		}
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
								fmt.Printf("  - %s", FormatFunctionSignature(fn))
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

// extractPackageName extracts the package name after the -p flag in a compile command
func extractPackageName(cmd *Command) string {
	// Find the -p flag
	for i, arg := range cmd.Args {
		if arg == "-p" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
	}
	return ""
}
