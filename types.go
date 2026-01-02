package main

// BuildAction represents a JSON entry from go build -json output
type BuildAction struct {
	ImportPath string `json:"ImportPath"`
	Action     string `json:"Action"`
	Output     string `json:"Output"`
	Package    string `json:"Package,omitempty"`
}

// Config holds all configuration options
type Config struct {
	LogFile         string
	DryRun          bool
	Dump            bool
	Verbose         bool
	Execute         bool
	Interactive     bool
	Capture         bool
	JSONCapture     bool
	PackFiles       bool
	PackFunctions   bool
	PackageNames    bool
	CallGraph       bool
	WorkDir         bool
	PackPackagePath bool
	Compile         bool
	HooksFiles      []string // Multiple hooks files (comma-separated or multiple --compile flags)
	SourceMappings  bool
}

// Capturer interface for different capture methods
type Capturer interface {
	Capture() error
	GetDescription() string
}
