package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
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
	case "pack-packagepath":
		fmt.Println("=== Pack Package Path Mode ===")
		compileCount := 0
		packageInfo := extractPackagePathInfo(commands)

		// Count compile commands
		for _, cmd := range commands {
			if isCompileCommand(&cmd) {
				compileCount++
			}
		}

		if len(packageInfo) > 0 {
			fmt.Printf("Found %d unique packages with paths in %d compile commands:\n\n", len(packageInfo), compileCount)
			for pkg, info := range packageInfo {
				fmt.Printf("  - Package: %s\n", pkg)
				fmt.Printf("    Path: %s\n", info.Path)
				fmt.Printf("    Work: %s\n", info.BuildID)
			}
		} else {
			fmt.Println("No package paths found in compile commands.")
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
	case "callgraph":
		fmt.Println("=== Call Graph Mode ===")
		compileCount := 0
		var allFiles []string

		// Collect all Go files from compile commands
		for _, cmd := range commands {
			if isCompileCommand(&cmd) {
				compileCount++
				files := extractPackFiles(&cmd)
				for _, file := range files {
					if strings.HasSuffix(file, ".go") {
						allFiles = append(allFiles, file)
					}
				}
			}
		}

		if len(allFiles) > 0 {
			// Get package information to filter only current module functions
			packageInfo, err := getPackageInfo(".")
			if err != nil {
				fmt.Printf("Warning: Could not load package info: %v\n", err)
				fmt.Println("Building call graph without package filtering...")
				packageInfo = nil
			}

			// Build the call graph with package filtering
			callGraph, err := BuildCallGraphWithPackageFilter(allFiles, packageInfo)
			if err != nil {
				fmt.Printf("Error building call graph: %v\n", err)
			} else {
				// Format and display the call graph
				var output string
				if packageInfo != nil {
					output = FormatCallGraphWithFilter(callGraph, packageInfo)
				} else {
					output = FormatCallGraph(callGraph)
				}
				fmt.Print(output)
			}
		} else {
			fmt.Println("No Go files found in compile commands.")
		}

		if compileCount > 0 {
			fmt.Printf("Processed %d compile commands with %d Go files.\n", compileCount, len(allFiles))
		} else {
			fmt.Println("No compile commands found.")
		}
	case "workdir":
		fmt.Println("=== Work Directory Mode ===")
		if len(commands) == 0 {
			fmt.Println("No commands found in log file.")
			break
		}

		// Get the first command
		firstCmd := commands[0]
		fmt.Printf("First command: %s\n", firstCmd.Raw)

		// Extract WORK= environment variable
		workDir := extractWorkDir(firstCmd.Raw)
		if workDir == "" {
			fmt.Println("No WORK= environment variable found in first command.")
			break
		}

		fmt.Printf("Found WORK directory: %s\n\n", workDir)

		// Dump all directories and files in the work directory
		if err := dumpWorkDir(workDir); err != nil {
			fmt.Printf("Error dumping work directory: %v\n", err)
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

// PackagePathInfo holds package path and build information
type PackagePathInfo struct {
	Path    string
	BuildID string
}

// extractWorkDir extracts the WORK= environment variable from a command string
func extractWorkDir(cmdLine string) string {
	// Use regex to find WORK=<path> in the command line
	re := regexp.MustCompile(`WORK=([^\s]+)`)
	matches := re.FindStringSubmatch(cmdLine)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// extractBuildID extracts the build ID from the -o flag value (e.g., b107 from $WORK/b107/_pkg_.a)
func extractBuildID(outputPath string) string {
	// Pattern to match $WORK/b###/_pkg_.a or similar
	re := regexp.MustCompile(`\$WORK/([^/]+)/`)
	matches := re.FindStringSubmatch(outputPath)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// extractOutputPath extracts the path after the -o flag in a compile command
func extractOutputPath(cmd *Command) string {
	// Find the -o flag
	for i, arg := range cmd.Args {
		if arg == "-o" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
	}
	return ""
}

// extractPackagePathInfo extracts package names and their common source paths from compile commands
func extractPackagePathInfo(commands []Command) map[string]PackagePathInfo {
	packageInfo := make(map[string]PackagePathInfo)
	packageFiles := make(map[string][]string)  // Package name -> list of file paths
	packageBuildIDs := make(map[string]string) // Package name -> build ID

	// Collect all files and build IDs for each package
	for _, cmd := range commands {
		if isCompileCommand(&cmd) {
			packageName := extractPackageName(&cmd)
			if packageName != "" {
				// Extract build ID from -o flag
				outputPath := extractOutputPath(&cmd)
				if outputPath != "" {
					buildID := extractBuildID(outputPath)
					if buildID != "" {
						packageBuildIDs[packageName] = buildID
					}
				}

				// Extract files
				files := extractPackFiles(&cmd)
				for _, file := range files {
					if strings.HasSuffix(file, ".go") {
						if packageFiles[packageName] == nil {
							packageFiles[packageName] = []string{}
						}
						packageFiles[packageName] = append(packageFiles[packageName], file)
					}
				}
			}
		}
	}

	// Find common path for each package and combine with build ID
	for pkg, files := range packageFiles {
		info := PackagePathInfo{}
		if len(files) > 0 {
			// Find the common directory path for all files in this package
			info.Path = findCommonPath(files)
		}
		// Add build ID if found
		if buildID, ok := packageBuildIDs[pkg]; ok {
			info.BuildID = buildID
		}
		packageInfo[pkg] = info
	}

	return packageInfo
}

// findCommonPath finds the common directory path for a list of file paths
func findCommonPath(files []string) string {
	if len(files) == 0 {
		return ""
	}

	// For a single file, just return its directory
	if len(files) == 1 {
		dir := filepath.Dir(files[0])
		// Convert relative path to absolute
		if !filepath.IsAbs(dir) {
			if absDir, err := filepath.Abs(dir); err == nil {
				dir = absDir
			}
		}
		return dir
	}

	// Start with the directory of the first file
	commonDir := filepath.Dir(files[0])

	// Check each subsequent file to find the common prefix
	for _, file := range files[1:] {
		fileDir := filepath.Dir(file)

		// Find the longest common directory prefix
		commonDir = findLongestCommonDir(commonDir, fileDir)

		// If we've reduced to nothing or current directory, stop
		if commonDir == "" || commonDir == "." {
			break
		}
	}

	// Convert relative path to absolute before returning
	if !filepath.IsAbs(commonDir) {
		if absDir, err := filepath.Abs(commonDir); err == nil {
			commonDir = absDir
		}
	}

	return commonDir
}

// findLongestCommonDir finds the longest common directory between two paths
func findLongestCommonDir(dir1, dir2 string) string {
	// Handle absolute paths properly
	if filepath.IsAbs(dir1) && filepath.IsAbs(dir2) {
		// Clean the paths first
		dir1 = filepath.Clean(dir1)
		dir2 = filepath.Clean(dir2)

		// Split by separator
		parts1 := strings.Split(dir1, string(filepath.Separator))
		parts2 := strings.Split(dir2, string(filepath.Separator))

		// Find common parts
		var common []string
		minLen := len(parts1)
		if len(parts2) < minLen {
			minLen = len(parts2)
		}

		for i := 0; i < minLen; i++ {
			if parts1[i] == parts2[i] {
				common = append(common, parts1[i])
			} else {
				break
			}
		}

		if len(common) == 0 {
			return ""
		}

		// For absolute paths on Unix, need to handle the leading slash
		if dir1[0] == '/' && len(common) > 0 && common[0] == "" {
			// First element is empty due to leading slash
			if len(common) == 1 {
				return "/"
			}
			return "/" + filepath.Join(common[1:]...)
		}

		return filepath.Join(common...)
	}

	// For relative paths or mixed absolute/relative, use simpler logic
	if dir1 == dir2 {
		return dir1
	}

	// Try to find common prefix by going up directories
	for dir1 != "" && dir1 != "." && dir1 != "/" {
		if strings.HasPrefix(dir2, dir1) {
			return dir1
		}
		dir1 = filepath.Dir(dir1)
	}

	return ""
}

// dumpWorkDir recursively dumps all directories and files in the specified work directory
func dumpWorkDir(workDir string) error {
	// Check if the work directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return fmt.Errorf("work directory does not exist: %s", workDir)
	}

	fmt.Printf("Contents of work directory: %s\n", workDir)
	fmt.Println("=" + strings.Repeat("=", len(workDir)+27))

	// Walk through the directory tree
	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error accessing %s: %v\n", path, err)
			return nil // Continue walking
		}

		// Calculate relative path from work directory
		relPath, err := filepath.Rel(workDir, path)
		if err != nil {
			relPath = path
		}

		// Calculate indentation level based on directory depth
		depth := strings.Count(relPath, string(filepath.Separator))
		indent := strings.Repeat("  ", depth)

		if info.IsDir() {
			if relPath == "." {
				// Skip the root directory itself
				return nil
			}
			fmt.Printf("%s📁 %s/\n", indent, filepath.Base(path))
		} else {
			// Show file with size
			fmt.Printf("%s📄 %s (%d bytes)\n", indent, filepath.Base(path), info.Size())
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking directory: %w", err)
	}

	return nil
}
