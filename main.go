package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	// Parse command line flags
	config := ParseFlags()

	// Handle capture mode if requested
	if config.ShouldCapture() {
		capturer := CreateCapturer(config)
		if err := capturer.Capture(); err != nil {
			log.Fatalf("Error during capture: %v", err)
		}
		fmt.Println(capturer.GetDescription())
		fmt.Println()
	}

	// Create and run processor
	processor := NewProcessor(config)
	if err := processor.Run(); err != nil {
		log.Fatalf("Error during processing: %v", err)
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
	// Parse the log file
	if err := p.parser.ParseFile(p.config.LogFile); err != nil {
		return fmt.Errorf("error parsing file: %w", err)
	}

	commands := p.parser.GetCommands()
	fmt.Printf("Parsed %d commands from %s\n\n", len(commands), p.config.LogFile)

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
