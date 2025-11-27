package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	var logFile string
	var dryRun bool
	var dump bool
	var verbose bool
	var execute bool
	var interactive bool

	flag.StringVar(&logFile, "log", "log", "Path to the log file to replay")
	flag.BoolVar(&dryRun, "dry-run", false, "Show commands without executing them")
	flag.BoolVar(&dump, "dump", false, "Dump parsed commands to console")
	flag.BoolVar(&verbose, "verbose", false, "Show detailed command information")
	flag.BoolVar(&execute, "execute", false, "Execute the generated script")
	flag.BoolVar(&interactive, "interactive", false, "Execute commands one by one interactively")
	flag.Parse()

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

	if verbose {
		parser.DumpCommands()
	} else if dump {
		for i, cmd := range commands {
			fmt.Printf("# Command %d\n", i+1)
			fmt.Println(cmd.String())
		}
	} else if dryRun {
		// Dry run mode - just show what would be executed
		fmt.Println("=== Dry Run Mode ===")
		for i, cmd := range commands {
			if cmd.Executable == "" {
				continue
			}
			fmt.Printf("Command %d: %s\n", i+1, cmd.String())
		}
	} else if interactive {
		// Interactive mode - execute commands one by one
		err := parser.ExecuteInteractive()
		if err != nil {
			log.Printf("Error in interactive mode: %v", err)
		}
	} else if execute {
		// Execute the script after generating it
		fmt.Println("=== Generating and Executing Script ===")
		err := parser.ExecuteAll()
		if err != nil {
			log.Printf("Error executing commands: %v", err)
		} else {
			fmt.Println("\nReplay completed successfully!")
		}
	} else {
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
