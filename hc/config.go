package main

import (
	"flag"
	"strings"
)

// stringSliceFlag is a custom flag type that allows multiple values
// Either comma-separated or multiple flags with the same name
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	// Support comma-separated values
	if strings.Contains(value, ",") {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				*s = append(*s, part)
			}
		}
	} else {
		*s = append(*s, value)
	}
	return nil
}

// ParseFlags parses command line flags and returns a Config struct
func ParseFlags() *Config {
	config := &Config{}

	// Custom flag for multiple hooks files
	var hooksFiles stringSliceFlag

	flag.StringVar(&config.LogFile, "log", "build-metadata/go-build.log", "Path to the log file to replay")
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
	flag.Var(&hooksFiles, "compile", "Parse hooks file(s) and match against functions in compile commands (can be specified multiple times or comma-separated)")
	flag.Var(&hooksFiles, "c", "Parse hooks file(s) and match against functions in compile commands (short for --compile)")
	flag.BoolVar(&config.SourceMappings, "source-mappings", false, "Generate source-mappings.json from existing go-build.log (for dlv debugger)")

	flag.Parse()

	// Copy hooks files to config
	config.HooksFiles = hooksFiles

	// If HooksFiles is provided, set Compile to true
	if len(config.HooksFiles) > 0 {
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
