package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Command struct {
	Raw         string
	Executable  string
	Args        []string
	IsMultiline bool
}

type Parser struct {
	commands []Command
}

func NewParser() *Parser {
	return &Parser{
		commands: make([]Command, 0),
	}
}

func (p *Parser) ParseFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return p.ParseReader(file)
}

func (p *Parser) ParseReader(r io.Reader) error {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if strings.Contains(line, "cat >") && strings.Contains(line, "<< 'EOF'") {
			cmd, err := p.parseHeredocCommand(line, scanner)
			if err != nil {
				return fmt.Errorf("failed to parse heredoc: %w", err)
			}
			p.commands = append(p.commands, cmd)
		} else {
			cmd := p.parseSingleLineCommand(line)
			p.commands = append(p.commands, cmd)
		}
	}

	return scanner.Err()
}

func (p *Parser) parseHeredocCommand(startLine string, scanner *bufio.Scanner) (Command, error) {
	// Remove any comment from the heredoc start line
	cleanStartLine := startLine
	if idx := strings.Index(startLine, " # "); idx != -1 {
		cleanStartLine = startLine[:idx]
	}

	var fullCommand strings.Builder
	fullCommand.WriteString(cleanStartLine)
	fullCommand.WriteString("\n")

	for scanner.Scan() {
		line := scanner.Text()
		fullCommand.WriteString(line)
		fullCommand.WriteString("\n")

		if strings.TrimSpace(line) == "EOF" {
			break
		}
	}

	raw := fullCommand.String()

	parts := strings.Fields(cleanStartLine)
	if len(parts) < 2 {
		return Command{}, fmt.Errorf("invalid heredoc command: %s", startLine)
	}

	return Command{
		Raw:         raw,
		Executable:  parts[0],
		Args:        parts[1:],
		IsMultiline: true,
	}, nil
}

func (p *Parser) parseSingleLineCommand(line string) Command {
	cleanLine := line
	if idx := strings.Index(line, " # "); idx != -1 {
		cleanLine = line[:idx]
	}

	cleanLine = strings.TrimSpace(cleanLine)

	if cleanLine == "" {
		return Command{Raw: line}
	}

	parts := parseCommandLine(cleanLine)

	if len(parts) == 0 {
		return Command{Raw: line}
	}

	return Command{
		Raw:         line,
		Executable:  parts[0],
		Args:        parts[1:],
		IsMultiline: false,
	}
}

func parseCommandLine(line string) []string {
	var result []string
	var current strings.Builder
	inQuote := false
	escapeNext := false

	for i, r := range line {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' {
			escapeNext = true
			continue
		}

		if r == '"' || r == '\'' {
			inQuote = !inQuote
			continue
		}

		if r == ' ' && !inQuote {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}

		if i == len(line)-1 && current.Len() > 0 {
			result = append(result, current.String())
		}
	}

	return result
}

func (p *Parser) GetCommands() []Command {
	return p.commands
}

func (p *Parser) GenerateScript() error {
	if len(p.commands) == 0 {
		return nil
	}

	// Create a single shell script with all commands
	var script strings.Builder
	script.WriteString("#!/bin/bash\n")
	script.WriteString("set -e  # Exit on any error\n\n")
	script.WriteString("echo \"Starting build replay...\"\n\n")

	for _, cmd := range p.commands {
		cmdStr := cmd.String()
		if cmdStr != "" {
			script.WriteString(cmdStr)
			script.WriteString("\n")
		}
	}

	script.WriteString("\necho \"Build replay completed!\"\n")

	// Write script to file
	scriptFile, err := os.Create("replay_script.sh")
	if err != nil {
		return fmt.Errorf("failed to create script file: %w", err)
	}
	defer scriptFile.Close()

	_, err = scriptFile.WriteString(script.String())
	if err != nil {
		return fmt.Errorf("failed to write script file: %w", err)
	}

	// Make the script executable
	err = os.Chmod("replay_script.sh", 0755)
	if err != nil {
		return fmt.Errorf("failed to make script executable: %w", err)
	}

	fmt.Printf("Generated executable script saved to: replay_script.sh\n")
	return nil
}

func (p *Parser) ExecuteAll() error {
	// First generate the script
	err := p.GenerateScript()
	if err != nil {
		return err
	}

	// Then execute it
	return p.ExecuteScript()
}

func (p *Parser) ExecuteScript() error {
	// Execute the generated script
	shellCmd := exec.Command("sh", "replay_script.sh")
	shellCmd.Stdout = os.Stdout
	shellCmd.Stderr = os.Stderr
	shellCmd.Stdin = os.Stdin

	return shellCmd.Run()
}

func (p *Parser) ExecuteInteractive() error {
	if len(p.commands) == 0 {
		fmt.Println("No commands to execute.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("=== Interactive Mode ===")
	fmt.Println("Commands will be executed one by one. You can:")
	fmt.Println("  y/yes/enter - Execute this command")
	fmt.Println("  n/no        - Skip this command")
	fmt.Println("  q/quit      - Quit interactive mode")
	fmt.Println("  s/show      - Show the command without executing")
	fmt.Println()

	// Start a persistent bash shell
	shellCmd := exec.Command("bash")
	stdin, err := shellCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	shellCmd.Stdout = os.Stdout
	shellCmd.Stderr = os.Stderr

	if err := shellCmd.Start(); err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}
	defer shellCmd.Process.Kill()

	// Set up the shell to exit on errors
	fmt.Fprintln(stdin, "set -e")

	executed := 0
	skipped := 0

	for i, cmd := range p.commands {
		cmdStr := cmd.String()
		if cmdStr == "" {
			continue
		}

		fmt.Printf("Command %d/%d:\n", i+1, len(p.commands))

		// Show a shortened version of long commands
		displayCmd := cmdStr
		if len(displayCmd) > 100 {
			displayCmd = displayCmd[:97] + "..."
		}
		fmt.Printf("  %s\n", displayCmd)

		for {
			fmt.Print("Execute? [y/n/q/s]: ")
			input, err := reader.ReadString('\n')
			if err != nil {
				stdin.Close()
				shellCmd.Wait()
				return fmt.Errorf("error reading input: %w", err)
			}

			input = strings.TrimSpace(strings.ToLower(input))

			switch input {
			case "", "y", "yes":
				fmt.Printf("Executing: %s\n", cmdStr)
				
				// Execute command in the persistent shell
				_, err := fmt.Fprintln(stdin, cmdStr)
				if err != nil {
					fmt.Printf("Error sending command to shell: %v\n", err)
					fmt.Print("Continue anyway? [y/n]: ")
					continueInput, _ := reader.ReadString('\n')
					continueInput = strings.TrimSpace(strings.ToLower(continueInput))
					if continueInput == "n" || continueInput == "no" {
						stdin.Close()
						shellCmd.Wait()
						return fmt.Errorf("execution stopped by user after error")
					}
				} else {
					// Give the command a moment to execute
					// This is a simple approach; for more robust handling,
					// we'd need to implement proper output synchronization
					fmt.Println("✓ Command sent to shell")
				}
				executed++
				goto nextCommand

			case "n", "no":
				fmt.Println("⊝ Skipped")
				skipped++
				goto nextCommand

			case "q", "quit":
				fmt.Printf("\nInteractive mode stopped by user.\n")
				fmt.Printf("Commands executed: %d, skipped: %d\n", executed, skipped)
				stdin.Close()
				shellCmd.Wait()
				return nil

			case "s", "show":
				fmt.Printf("Full command:\n%s\n", cmdStr)
				// Continue the loop to ask again

			default:
				fmt.Println("Invalid input. Use y/n/q/s")
				// Continue the loop to ask again
			}
		}

	nextCommand:
		fmt.Println()
	}

	// Close stdin to signal the shell to exit
	stdin.Close()
	shellCmd.Wait()

	fmt.Printf("Interactive execution completed!\n")
	fmt.Printf("Commands executed: %d, skipped: %d\n", executed, skipped)
	return nil
}

func (p *Parser) DumpCommands() {
	for i, cmd := range p.commands {
		fmt.Printf("Command %d:\n", i+1)
		if cmd.IsMultiline {
			fmt.Printf("  Type: Multiline (Heredoc)\n")
			fmt.Printf("  Raw:\n%s\n", indent(cmd.Raw, "    "))
		} else {
			fmt.Printf("  Type: Single Line\n")
			fmt.Printf("  Executable: %s\n", cmd.Executable)
			if len(cmd.Args) > 0 {
				fmt.Printf("  Args: %v\n", cmd.Args)
			}
			fmt.Printf("  Raw: %s\n", cmd.Raw)
		}
		fmt.Println()
	}
}

func indent(text string, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

func (c *Command) Execute() error {
	commandStr := c.String()
	if commandStr == "" {
		return nil
	}

	cmd := exec.Command("bash", "-c", commandStr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Command) String() string {
	if c.IsMultiline {
		return c.Raw
	}
	if c.Executable == "" {
		return c.Raw
	}

	// Check if the raw command contains shell redirection operators
	// In these cases, we should use the raw command instead of reconstructing
	cleanRaw := c.Raw
	if idx := strings.Index(c.Raw, " # "); idx != -1 {
		cleanRaw = strings.TrimSpace(c.Raw[:idx])
	}
	
	if strings.ContainsAny(cleanRaw, "><|") {
		return cleanRaw
	}

	// Quote arguments that need it for non-redirection commands
	quotedArgs := make([]string, len(c.Args))
	for i, arg := range c.Args {
		// Check if argument needs quoting (contains special characters)
		if strings.Contains(arg, "$") && strings.Contains(arg, "=>") {
			// Quote arguments like $WORK/b014=>
			quotedArgs[i] = fmt.Sprintf("\"%s\"", arg)
		} else if strings.ContainsAny(arg, " \t'\"\\") {
			// Quote arguments with spaces or special chars
			quotedArgs[i] = fmt.Sprintf("\"%s\"", arg)
		} else {
			quotedArgs[i] = arg
		}
	}

	return fmt.Sprintf("%s %s", c.Executable, strings.Join(quotedArgs, " "))
}
