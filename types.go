package main

import (
	"os"
	"path/filepath"
)

// MetadataDir is the directory where all build metadata files are stored
const MetadataDir = "build-metadata"

// MetadataFile names
const (
	BuildLogFile         = "go-build.log"
	BuildJSONFile        = "go-build.json"
	BuildModifiedLogFile = "go-build-modified.log"
	ReplayScriptFile     = "replay_script.sh"
	SourceMappingsFile   = "source-mappings.json"
)

// GetMetadataPath returns the full path to a metadata file
func GetMetadataPath(filename string) string {
	return filepath.Join(MetadataDir, filename)
}

// EnsureMetadataDir creates the metadata directory if it doesn't exist
func EnsureMetadataDir() error {
	return os.MkdirAll(MetadataDir, 0755)
}

// EnsureMetadataDirIn creates the metadata directory in a specific base directory
func EnsureMetadataDirIn(baseDir string) error {
	return os.MkdirAll(filepath.Join(baseDir, MetadataDir), 0755)
}

// GetMetadataPathIn returns the full path to a metadata file in a specific base directory
func GetMetadataPathIn(baseDir, filename string) string {
	return filepath.Join(baseDir, MetadataDir, filename)
}

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
