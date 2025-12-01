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

	flag.Parse()
	return config
}

// GetExecutionMode returns the execution mode based on config flags
func (c *Config) GetExecutionMode() string {
	switch {
	case c.JSONCapture:
		return "json-capture"
	case c.Capture:
		return "capture"
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
