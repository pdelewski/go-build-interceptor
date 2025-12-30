package main

import (
	"flag"
)

// ParseFlags parses command line flags and returns a Config struct
func ParseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.LogFile, "log", "go-build.log", "Path to the log file to replay")
	flag.BoolVar(&config.DryRun, "dry-run", false, "Show commands without executing them")
	flag.BoolVar(&config.Dump, "dump", false, "Dump parsed commands to console")
	flag.BoolVar(&config.Verbose, "verbose", false, "Show detailed command information")
	flag.BoolVar(&config.Execute, "execute", false, "Execute the generated script")
	flag.BoolVar(&config.Interactive, "interactive", false, "Execute commands one by one interactively")
	flag.BoolVar(&config.Capture, "capture", false, "Capture go build output to go-build.log")
	flag.BoolVar(&config.JSONCapture, "json", false, "Capture go build JSON output and convert to text format in go-build.log")
	flag.BoolVar(&config.PackFiles, "pack-files", false, "Process and display files from compile commands with -pack flag")
	flag.BoolVar(&config.PackFunctions, "pack-functions", false, "Extract and display functions from Go files in compile commands with -pack flag")
	flag.BoolVar(&config.PackageNames, "pack-packages", false, "Extract and display package names from compile commands with -p flag")
	flag.BoolVar(&config.CallGraph, "callgraph", false, "Generate and display call graph from Go files in compile commands")
	flag.BoolVar(&config.WorkDir, "workdir", false, "Check first command and extract WORK directory, then dump all directories and files there")
	flag.BoolVar(&config.PackPackagePath, "pack-packagepath", false, "Extract and display package names with their source paths from compile commands")
	flag.StringVar(&config.HooksFile, "compile", "", "Parse hooks file and match against functions in compile commands")
	flag.StringVar(&config.HooksFile, "c", "", "Parse hooks file and match against functions in compile commands (short for --compile)")
	flag.BoolVar(&config.SourceMappings, "source-mappings", false, "Generate source-mappings.json from existing go-build.log (for dlv debugger)")

	flag.Parse()

	// If HooksFile is provided, set Compile to true
	if config.HooksFile != "" {
		config.Compile = true
	}
	return config
}

// GetExecutionMode returns the execution mode based on config flags
func (c *Config) GetExecutionMode() string {
	switch {
	case c.JSONCapture:
		return "json-capture"
	case c.Capture:
		return "capture"
	case c.Compile:
		return "compile"
	case c.SourceMappings:
		return "source-mappings"
	case c.WorkDir:
		return "workdir"
	case c.PackPackagePath:
		return "pack-packagepath"
	case c.CallGraph:
		return "callgraph"
	case c.PackageNames:
		return "pack-packages"
	case c.PackFunctions:
		return "pack-functions"
	case c.PackFiles:
		return "pack-files"
	case c.Verbose:
		return "verbose"
	case c.Dump:
		return "dump"
	case c.DryRun:
		return "dry-run"
	case c.Interactive:
		return "interactive"
	case c.Execute:
		return "execute"
	default:
		return "generate"
	}
}
