package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// TextCapturer captures go build output in text format
type TextCapturer struct{}

// Capture runs go build and captures text output to build-metadata/go-build.log
func (t *TextCapturer) Capture() error {
	if err := EnsureMetadataDir(); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	logPath := GetMetadataPath(BuildLogFile)
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", logPath, err)
	}
	defer logFile.Close()

	fmt.Println("Running: go build -x -a -work")
	cmd := exec.Command("go", "build", "-x", "-a", "-work")

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err = cmd.Run()
	if err != nil {
		fmt.Printf("Note: go build exited with error: %v\n", err)
		fmt.Printf("But build commands have been captured to %s\n", logPath)
	}

	return nil
}

// GetDescription returns a description of what this capturer does
func (t *TextCapturer) GetDescription() string {
	return "Captured go build output to go-build.log"
}

// JSONCapturer captures go build JSON output and converts to text format
type JSONCapturer struct{}

// Capture runs go build with JSON output, saves raw JSON, and converts to text
func (j *JSONCapturer) Capture() error {
	if err := EnsureMetadataDir(); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	fmt.Println("Running: go build -x -a -work -json")
	cmd := exec.Command("go", "build", "-x", "-a", "-work", "-json")

	jsonOutput, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Note: go build exited with error: %v\n", err)
		fmt.Println("But continuing with captured JSON output...")
	}

	// Save raw JSON output
	if err := saveRawJSON(jsonOutput); err != nil {
		return err
	}

	// Extract outputs and convert to text format
	outputs, err := extractOutputsFromJSON(jsonOutput)
	if err != nil {
		return err
	}

	// Write to go-build.log
	if err := writeTextOutput(outputs); err != nil {
		return err
	}

	logPath := GetMetadataPath(BuildLogFile)
	fmt.Printf("Extracted %d commands from JSON and saved to %s\n", len(outputs), logPath)
	return nil
}

// GetDescription returns a description of what this capturer does
func (j *JSONCapturer) GetDescription() string {
	return "Captured JSON build output, converted to text format in go-build.log"
}

// saveRawJSON saves the raw JSON output to build-metadata/go-build.json
func saveRawJSON(jsonOutput []byte) error {
	jsonPath := GetMetadataPath(BuildJSONFile)
	jsonFile, err := os.Create(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", jsonPath, err)
	}
	defer jsonFile.Close()

	_, err = jsonFile.Write(jsonOutput)
	if err != nil {
		return fmt.Errorf("failed to write JSON output: %w", err)
	}

	return nil
}

// extractOutputsFromJSON parses JSON and extracts Output fields
func extractOutputsFromJSON(jsonOutput []byte) ([]string, error) {
	var allOutputs []string
	scanner := bufio.NewScanner(strings.NewReader(string(jsonOutput)))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var buildAction BuildAction
		if err := json.Unmarshal([]byte(line), &buildAction); err != nil {
			// Skip non-JSON lines
			continue
		}

		if buildAction.Output != "" {
			allOutputs = append(allOutputs, buildAction.Output)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning JSON output: %w", err)
	}

	return allOutputs, nil
}

// writeTextOutput writes the extracted outputs to build-metadata/go-build.log
func writeTextOutput(outputs []string) error {
	logPath := GetMetadataPath(BuildLogFile)
	outputFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", logPath, err)
	}
	defer outputFile.Close()

	for _, output := range outputs {
		_, err := outputFile.WriteString(output)
		if err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
		// Add newline if the output doesn't end with one
		if !strings.HasSuffix(output, "\n") {
			_, err = outputFile.WriteString("\n")
			if err != nil {
				return fmt.Errorf("failed to write newline: %w", err)
			}
		}
	}

	return nil
}
