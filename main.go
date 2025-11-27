package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
)

func main() {
	var logFile string
	var dryRun bool
	var dump bool
	var verbose bool
	var execute bool
	var interactive bool
	var capture bool

	flag.StringVar(&logFile, "log", "go-build.log", "Path to the log file to replay")
	flag.BoolVar(&dryRun, "dry-run", false, "Show commands without executing them")
	flag.BoolVar(&dump, "dump", false, "Dump parsed commands to console")
	flag.BoolVar(&verbose, "verbose", false, "Show detailed command information")
	flag.BoolVar(&execute, "execute", false, "Execute the generated script")
	flag.BoolVar(&interactive, "interactive", false, "Execute commands one by one interactively")
	flag.BoolVar(&capture, "capture", false, "Capture go build output to go-build.log")
	flag.Parse()

	// If capture flag is set, run go build and capture output
	if capture {
		err := captureGoBuild()
		if err != nil {
			log.Fatalf("Error capturing go build output: %v", err)
		}
		fmt.Println("Captured go build output to go-build.log")
		fmt.Println()
	}

	// Use the new parser
	parser := NewParser()

	err := parser.ParseFile(logFile)
	if err != nil {
		log.Fatalf("Error parsing file: %v", err)
	}

	commands := parser.GetCommands()
	fmt.Printf("Parsed %d commands from %s\n\n", len(commands), logFile)

	// Set up WORK environment variable if not already set
	if os.Getenv("WORK") == "" && !dryRun && !dump && !verbose && (interactive || execute) {
		tmpDir, err := os.MkdirTemp("", "go-build-replay")
		if err != nil {
			log.Fatalf("Failed to create temp directory: %v", err)
		}
		os.Setenv("WORK", tmpDir)
		fmt.Printf("Created WORK directory: %s\n\n", tmpDir)
		defer func() {
			os.RemoveAll(tmpDir)
		}()
	}

	switch {
	case verbose:
		parser.DumpCommands()
	case dump:
		for i, cmd := range commands {
			fmt.Printf("# Command %d\n", i+1)
			fmt.Println(cmd.String())
		}
	case dryRun:
		// Dry run mode - just show what would be executed
		fmt.Println("=== Dry Run Mode ===")
		for i, cmd := range commands {
			if cmd.Executable == "" {
				continue
			}
			fmt.Printf("Command %d: %s\n", i+1, cmd.String())
		}
	case interactive:
		// Interactive mode - execute commands one by one
		err := parser.ExecuteInteractive()
		if err != nil {
			log.Printf("Error in interactive mode: %v", err)
		}
	case execute:
		// Execute the script after generating it
		fmt.Println("=== Generating and Executing Script ===")
		err := parser.ExecuteAll()
		if err != nil {
			log.Printf("Error executing commands: %v", err)
		} else {
			fmt.Println("\nReplay completed successfully!")
		}
	default:
		// Default behavior: generate script without executing
		fmt.Println("=== Generating Script ===")
		err := parser.GenerateScript()
		if err != nil {
			log.Printf("Error generating script: %v", err)
		} else {
			fmt.Println("\nScript generated successfully! Use --execute flag to run it.")
		}
	}
}

func captureGoBuild() error {
	// Create/open the log file
	logFile, err := os.Create("go-build.log")
	if err != nil {
		return fmt.Errorf("failed to create go-build.log: %w", err)
	}
	defer logFile.Close()

	// Run go build with -x -a -work flags
	fmt.Println("Running: go build -x -a -work")
	cmd := exec.Command("go", "build", "-x", "-a", "-work")

	// Capture both stdout and stderr
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Run the command
	err = cmd.Run()
	if err != nil {
		// Even if build fails, we may have captured useful output
		fmt.Printf("Note: go build exited with error: %v\n", err)
		fmt.Println("But build commands have been captured to go-build.log")
	}

	return nil
}
