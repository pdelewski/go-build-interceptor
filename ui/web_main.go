package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type FileRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type FileResponse struct {
	Success bool   `json:"success"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Global variable to store the root directory
var rootDirectory string

// Restrict navigation to root directory only (disabled by default for local use)
var restrictNavigation bool

// WebSocket upgrader for LSP
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local development
	},
}

// Global gopls process management
var (
	goplsCmd   *exec.Cmd
	goplsStdin io.WriteCloser
	goplsMutex sync.Mutex
)

// ensureGopls checks if gopls is installed and installs it if not
func ensureGopls() error {
	if _, err := exec.LookPath("gopls"); err != nil {
		log.Println("gopls not found, installing...")
		cmd := exec.Command("go", "install", "golang.org/x/tools/gopls@latest")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install gopls: %v", err)
		}
		log.Println("gopls installed successfully")
	}
	return nil
}

// ensureBuildLog checks if go-build.log exists and captures it if not
func ensureBuildLog() error {
	buildLogPath := filepath.Join(rootDirectory, "build-metadata", "go-build.log")

	// Check if go-build.log already exists
	if _, err := os.Stat(buildLogPath); err == nil {
		log.Printf("Build log already exists: %s\n", buildLogPath)
		return nil
	}

	log.Printf("Build log not found, capturing build output for: %s\n", rootDirectory)

	// Get absolute path to hc executable
	execPath, err := filepath.Abs("../hc/hc")
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %v", err)
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		return fmt.Errorf("hc executable not found at: %s", execPath)
	}

	// Run hc --json to capture the build log
	log.Printf("Executing: %s --json\n", execPath)
	cmd := exec.Command(execPath, "--json")
	cmd.Dir = rootDirectory
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to capture build log: %v", err)
	}

	log.Println("Build log captured successfully")
	return nil
}

// startGopls starts the gopls language server process
func startGopls() (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	cmd := exec.Command("gopls", "serve")
	cmd.Dir = rootDirectory

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get stdin pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, nil, nil, fmt.Errorf("failed to get stdout pipe: %v", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, nil, nil, fmt.Errorf("failed to start gopls: %v", err)
	}

	log.Printf("Started gopls (PID: %d) for directory: %s\n", cmd.Process.Pid, rootDirectory)
	return cmd, stdin, stdout, nil
}

// handleLSPWebSocket handles WebSocket connections for LSP communication
func handleLSPWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v\n", err)
		return
	}
	defer conn.Close()

	log.Println("LSP WebSocket connection established")

	// Start gopls for this connection
	cmd, stdin, stdout, err := startGopls()
	if err != nil {
		log.Printf("Failed to start gopls: %v\n", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"error": "%s"}`, err.Error())))
		return
	}
	defer func() {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		log.Println("gopls process terminated")
	}()

	// Create a channel to signal when to stop
	done := make(chan struct{})
	defer close(done)

	// Goroutine to read from gopls stdout and send to WebSocket
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			select {
			case <-done:
				return
			default:
				// Read Content-Length header
				header, err := reader.ReadString('\n')
				if err != nil {
					if err != io.EOF {
						log.Printf("Error reading from gopls: %v\n", err)
					}
					return
				}

				// Parse Content-Length
				if !strings.HasPrefix(header, "Content-Length:") {
					continue
				}
				var contentLength int
				fmt.Sscanf(header, "Content-Length: %d", &contentLength)

				// Read empty line
				_, err = reader.ReadString('\n')
				if err != nil {
					return
				}

				// Read content
				content := make([]byte, contentLength)
				_, err = io.ReadFull(reader, content)
				if err != nil {
					log.Printf("Error reading content from gopls: %v\n", err)
					return
				}

				// Send to WebSocket
				if err := conn.WriteMessage(websocket.TextMessage, content); err != nil {
					log.Printf("Error writing to WebSocket: %v\n", err)
					return
				}
			}
		}
	}()

	// Read from WebSocket and send to gopls stdin
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v\n", err)
			}
			break
		}

		// Format as LSP message with Content-Length header
		lspMessage := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(message), message)
		if _, err := stdin.Write([]byte(lspMessage)); err != nil {
			log.Printf("Error writing to gopls: %v\n", err)
			break
		}
	}
}

func main() {
	// Parse command line flags
	flag.StringVar(&rootDirectory, "dir", ".", "Root directory to serve files from")
	port := flag.String("port", "9090", "Port to serve on")
	flag.BoolVar(&restrictNavigation, "restrict-nav", false, "Restrict file navigation to root directory only")
	flag.Parse()

	// Resolve the root directory to an absolute path
	absRoot, err := filepath.Abs(rootDirectory)
	if err != nil {
		log.Fatalf("Failed to resolve root directory: %v", err)
	}
	rootDirectory = absRoot

	// Verify the root directory exists
	if _, err := os.Stat(rootDirectory); os.IsNotExist(err) {
		log.Fatalf("Root directory does not exist: %s", rootDirectory)
	}

	// Ensure gopls is installed
	if err := ensureGopls(); err != nil {
		log.Printf("Warning: %v\n", err)
		log.Println("LSP features will not be available")
	}

	// Ensure build log exists (capture if missing)
	if err := ensureBuildLog(); err != nil {
		log.Printf("Warning: %v\n", err)
		log.Println("Some features may not work without a build log")
	}

	// Serve static files from the static directory
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))

	// Main editor page
	http.HandleFunc("/", serveEditor)

	// API endpoints
	http.HandleFunc("/api/open", openFile)
	http.HandleFunc("/api/save", saveFile)
	http.HandleFunc("/api/list", listFiles)
	http.HandleFunc("/api/pack-files", getPackFiles)
	http.HandleFunc("/api/pack-functions", getPackFunctions)
	http.HandleFunc("/api/pack-packages", getPackPackages)
	http.HandleFunc("/api/callgraph", getCallGraph)
	http.HandleFunc("/api/workdir", getWorkDir)
	http.HandleFunc("/api/compile", getCompile)
	http.HandleFunc("/api/run-executable", getRunExecutable)
	http.HandleFunc("/api/create-hooks-module", createHooksModule)
	http.HandleFunc("/api/debug", handleDebug)
	http.HandleFunc("/api/cleanup", handleCleanup)

	// LSP WebSocket endpoint
	http.HandleFunc("/ws/lsp", handleLSPWebSocket)

	// Debug WebSocket endpoint
	http.HandleFunc("/ws/debug", handleDebugWebSocket)

	// Run executable WebSocket endpoint (for real-time output)
	http.HandleFunc("/ws/run", handleRunWebSocket)

	// Stop process endpoint
	http.HandleFunc("/api/stop-process", handleStopProcess)

	fmt.Printf("🚀 Web Text Editor Server Starting...\n")
	fmt.Printf("📝 Access the editor at: http://localhost:%s\n", *port)
	fmt.Printf("📁 Root directory: %s\n", rootDirectory)
	fmt.Printf("⏹️  Press Ctrl+C to stop the server\n\n")

	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

// getFullPath resolves a relative path to a full path within the root directory
func getFullPath(relativePath string) (string, error) {
	// Clean the path to prevent directory traversal
	cleanPath := filepath.Clean(relativePath)

	// Join with root directory
	fullPath := filepath.Join(rootDirectory, cleanPath)

	// Only enforce root directory restriction if restrictNavigation is enabled
	if restrictNavigation && !strings.HasPrefix(fullPath, rootDirectory) {
		return "", fmt.Errorf("path outside root directory")
	}

	return fullPath, nil
}

func serveEditor(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Code Editor</title>
    <link rel="stylesheet" href="/static/editor.css">
    <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>💻</text></svg>">
    <!-- Monaco Editor -->
    <script src="/static/monaco/vs/loader.js"></script>
    <script>
        require.config({ paths: { vs: '/static/monaco/vs' } });
        // Root directory for LSP
        window.PROJECT_ROOT = '` + rootDirectory + `';
    </script>
</head>
<body class="vscode-theme">
    <!-- Top Menu Bar -->
    <div class="menu-bar">
        <div class="menu-items">
            <div class="menu-item" data-menu="file">
                File
                <div class="dropdown-menu">
                    <div class="menu-option" onclick="createNewFile()">
                        New File <span class="menu-shortcut">Ctrl+N</span>
                    </div>
                    <div class="menu-option" onclick="openFileDialog()">
                        Open... <span class="menu-shortcut">Ctrl+O</span>
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="saveCurrentFile()">
                        Save <span class="menu-shortcut">Ctrl+S</span>
                    </div>
                    <div class="menu-option" onclick="saveAsCurrentFile()">
                        Save As... <span class="menu-shortcut">Ctrl+Shift+S</span>
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="closeCurrentTab()">
                        Close Tab <span class="menu-shortcut">Ctrl+W</span>
                    </div>
                    <div class="menu-option" onclick="closeAllTabs()">
                        Close All Tabs
                    </div>
                </div>
            </div>
            <div class="menu-item" data-menu="edit">
                Edit
                <div class="dropdown-menu">
                    <div class="menu-option" onclick="undoAction()">
                        Undo <span class="menu-shortcut">Ctrl+Z</span>
                    </div>
                    <div class="menu-option" onclick="redoAction()">
                        Redo <span class="menu-shortcut">Ctrl+Y</span>
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="cutText()">
                        Cut <span class="menu-shortcut">Ctrl+X</span>
                    </div>
                    <div class="menu-option" onclick="copyText()">
                        Copy <span class="menu-shortcut">Ctrl+C</span>
                    </div>
                    <div class="menu-option" onclick="pasteText()">
                        Paste <span class="menu-shortcut">Ctrl+V</span>
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="selectAllText()">
                        Select All <span class="menu-shortcut">Ctrl+A</span>
                    </div>
                    <div class="menu-option" onclick="findInFile()">
                        Find <span class="menu-shortcut">Ctrl+F</span>
                    </div>
                </div>
            </div>
            <div class="menu-item" data-menu="view">
                View
                <div class="dropdown-menu">
                    <div class="menu-option" onclick="toggleExplorer()">
                        Toggle Explorer <span class="menu-shortcut">Ctrl+Shift+E</span>
                    </div>
                    <div class="menu-option" onclick="toggleSearch()">
                        Toggle Search <span class="menu-shortcut">Ctrl+Shift+F</span>
                    </div>
                    <div class="menu-option" onclick="toggleGitPanel()">
                        Toggle Git <span class="menu-shortcut">Ctrl+Shift+G</span>
                    </div>
                    <div class="menu-option" onclick="toggleTerminal()">
                        Toggle Terminal <span class="menu-shortcut">Ctrl+T</span>
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="showFunctions()">
                        Functions
                    </div>
                    <div class="menu-option" onclick="showFiles()">
                        Files
                    </div>
                    <div class="menu-option" onclick="showProject()">
                        Project
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="showStaticCallGraph()">
                        Static Call Graph
                    </div>
                    <div class="menu-option" onclick="showPackages()">
                        Packages
                    </div>
                    <div class="menu-option" onclick="showWorkDirectory()">
                        Work Directory
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="toggleWordWrap()">
                        Toggle Word Wrap
                    </div>
                    <div class="menu-option" onclick="zoomIn()">
                        Zoom In <span class="menu-shortcut">Ctrl++</span>
                    </div>
                    <div class="menu-option" onclick="zoomOut()">
                        Zoom Out <span class="menu-shortcut">Ctrl+-</span>
                    </div>
                </div>
            </div>
            <div class="menu-item" data-menu="help">
                Help
                <div class="dropdown-menu">
                    <div class="menu-option" onclick="showKeyboardShortcuts()">
                        Keyboard Shortcuts
                    </div>
                    <div class="menu-option" onclick="showAbout()">
                        About Code Editor
                    </div>
                    <div class="menu-separator"></div>
                    <div class="menu-option" onclick="openDocumentation()">
                        Documentation
                    </div>
                    <div class="menu-option" onclick="reportIssue()">
                        Report Issue
                    </div>
                </div>
            </div>
        </div>
        <div class="window-controls">
            <div class="window-title">Code Editor</div>
        </div>
    </div>

    <!-- Toolbar -->
    <div class="toolbar">
        <div class="toolbar-section">
            <button class="toolbar-button" onclick="createNewFile()" title="New File (Ctrl+N)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M9.5 1.1l3.4 3.5.1.4v2h-1V6H8V2H3v11h4v1H2.5l-.5-.5v-12l.5-.5h6.7l.3.1zM9 2v3h2.9L9 2zm4 14h-1v-3H9v-1h3V9h1v3h3v1h-3v3z"/></svg>
            </button>
            <button class="toolbar-button" onclick="openFileDialog()" title="Open File (Ctrl+O)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M2.5 2h5l.2.1.3.2L9.9 4H14.5l.5.5v10l-.5.5h-12l-.5-.5v-12l.5-.5zm.5 1v11h11V7H9.5l-.2-.1L9 6.7 7.1 5H3V3zm10 4v5H3V7h10z"/></svg>
            </button>
            <button class="toolbar-button" onclick="saveCurrentFile()" title="Save File (Ctrl+S)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M13.5 1h-11l-.5.5v13l.5.5H14.5l.5-.5V4.8l-.1-.3-.9-.9-2.1-2.1-.3-.1zM13 2v3H8V2h5zm1 13H2V2h5v3.5l.5.5H14v9zm-3-7.5a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0z"/></svg>
            </button>
            <div class="toolbar-separator"></div>
            <button class="toolbar-button" onclick="undoAction()" title="Undo (Ctrl+Z)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M8 2a6 6 0 1 1 0 12A6 6 0 0 1 8 2zm0 1a5 5 0 1 0 0 10A5 5 0 0 0 8 3zM6.5 5L4 7.5 6.5 10v-2h3a1.5 1.5 0 0 1 0 3H8v1h1.5a2.5 2.5 0 0 0 0-5h-3V5z"/></svg>
            </button>
            <button class="toolbar-button" onclick="redoAction()" title="Redo (Ctrl+Y)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M8 2a6 6 0 1 0 0 12A6 6 0 0 0 8 2zM8 3a5 5 0 1 1 0 10A5 5 0 0 1 8 3zm1.5 2v2h-3a1.5 1.5 0 0 0 0 3H8v1H6.5a2.5 2.5 0 0 1 0-5h3V5L12 7.5 9.5 10V8z"/></svg>
            </button>
            <div class="toolbar-separator"></div>
            <button class="toolbar-button" onclick="cutText()" title="Cut (Ctrl+X)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M3.5 2a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3zm9 0a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3zM3 5.5L1 13h3l1-3h6l1 3h3L13 5.5 11 9H5L3 5.5z"/></svg>
            </button>
            <button class="toolbar-button" onclick="copyText()" title="Copy (Ctrl+C)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M4 2v1H2.5l-.5.5v10l.5.5H10.5l.5-.5V12h1v2.5l-.5.5h-9l-.5-.5v-11l.5-.5H4zm2.5 0l.5.5v10l.5.5H14.5l.5-.5v-10l-.5-.5h-8zm.5 1h7v9H7V3z"/></svg>
            </button>
            <button class="toolbar-button" onclick="pasteText()" title="Paste (Ctrl+V)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M10 1v1h3.3l.4.2.3.3V14.5l-.5.5h-11l-.5-.5v-12l.3-.3.4-.2H6V1h4zm0 2v1H6V3H3v11h10V3h-3z"/></svg>
            </button>
        </div>
        <!-- Selection Controls Toolbar (shown when items selected in Functions/Call Graph views) -->
        <div id="selectionToolbar" class="toolbar-section toolbar-selection" style="display: none;">
            <div class="toolbar-separator"></div>
            <span id="selectionContext" style="color: #4fc3f7; font-size: 12px; margin-right: 8px; white-space: nowrap;"></span>
            <button class="toolbar-button toolbar-button-success" onclick="generateHooksFromSelection()" title="Generate Hooks File">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M14.773 3.485l-.78-.781a.5.5 0 0 0-.707 0L6.5 9.49l-2.793-2.792a.5.5 0 0 0-.707 0l-.78.781a.5.5 0 0 0 0 .707l3.926 3.927a.5.5 0 0 0 .707 0l7.92-7.921a.5.5 0 0 0 0-.707z"/></svg>
                <span style="margin-left: 4px;">Generate Hooks</span>
            </button>
            <button class="toolbar-button" onclick="selectAllFromToolbar()" title="Select All">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M3 3v10h10V3H3zm9 9H4V4h8v8z"/><path fill="currentColor" d="M6 6h4v4H6z"/></svg>
                <span style="margin-left: 4px;">All</span>
            </button>
            <button class="toolbar-button" onclick="clearSelectionFromToolbar()" title="Clear Selection">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M3 3v10h10V3H3zm9 9H4V4h8v8z"/></svg>
                <span style="margin-left: 4px;">Clear</span>
            </button>
        </div>
        <div class="toolbar-section toolbar-right">
            <button class="toolbar-button" onclick="findInFile()" title="Find in Files (Ctrl+F)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="m15.7 13.3-3.81-3.83A5.93 5.93 0 0 0 13 6c0-3.31-2.69-6-6-6S1 2.69 1 6s2.69 6 6 6c1.3 0 2.48-.41 3.47-1.11l3.83 3.81c.19.2.45.3.7.3.25 0 .52-.09.7-.3a.996.996 0 0 0 0-1.4ZM7 10.7c-2.59 0-4.7-2.11-4.7-4.7 0-2.59 2.11-4.7 4.7-4.7 2.59 0 4.7 2.11 4.7 4.7 0 2.59-2.11 4.7-4.7 4.7Z"/></svg>
            </button>
            <button class="toolbar-button" onclick="toggleWordWrap()" title="Toggle Word Wrap">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M2 3h13v1H2V3zm0 3h10v1H2V6zm0 3h13v1H2V9zm0 3h10v1H2v-1z"/></svg>
            </button>
            <div class="toolbar-separator"></div>
            <button class="toolbar-button" onclick="zoomOut()" title="Zoom Out (Ctrl+-)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M6.5 12a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11zm0-1a4.5 4.5 0 1 1 0-9 4.5 4.5 0 0 1 0 9zM4 6h5v1H4V6z"/></svg>
            </button>
            <button class="toolbar-button" onclick="zoomIn()" title="Zoom In (Ctrl++)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M6.5 12a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11zm0-1a4.5 4.5 0 1 1 0-9 4.5 4.5 0 0 1 0 9zM7 4v2h2v1H7v2H6V7H4V6h2V4h1z"/></svg>
            </button>
            <div class="toolbar-separator"></div>
            <button class="toolbar-button" onclick="toggleExplorer()" title="Toggle Explorer">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M14.5 3H7.71l-.85-.85L6.51 2h-5a.5.5 0 0 0-.5.5v11a.5.5 0 0 0 .5.5h13a.5.5 0 0 0 .5-.5v-10a.5.5 0 0 0-.5-.5Z"/></svg>
            </button>
            <button class="toolbar-button" onclick="toggleTerminal()" title="Toggle Terminal">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M2 2v12h12V2H2zm11 11H3V3h10v10zM5.8 9L4 7.2l.6-.6L6 8l3.5-3.5.6.6L6.6 8.5l-.8.5z"/></svg>
            </button>
            <div class="toolbar-separator"></div>
            <button class="toolbar-button" onclick="runCompile()" title="Run Compile with Hooks">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M2 14L14 8 2 2v5l10 1L2 9v5z"/></svg>
            </button>
            <button class="toolbar-button" onclick="runExecutable()" title="Run Built Executable">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M4 2v12l10-6L4 2z"/></svg>
            </button>
            <button class="toolbar-button" onclick="runDebug()" title="Debug with Delve">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M8 1a4 4 0 0 0-4 4v1H3v2h1v1.5L2.5 11 3 12l1.5-1H6v2.17A3.001 3.001 0 0 0 8 16a3.001 3.001 0 0 0 2-2.83V11h1.5l1.5 1 .5-1-1.5-1.5V8h1V6h-1V5a4 4 0 0 0-4-4zm-2 4a2 2 0 1 1 4 0v1H6V5zm0 3h4v3a2 2 0 1 1-4 0V8z"/></svg>
            </button>
            <button class="toolbar-button" onclick="runCleanup()" title="Clean Build Artifacts">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M5.5 5.5A.5.5 0 0 1 6 6v6a.5.5 0 0 1-1 0V6a.5.5 0 0 1 .5-.5zm2.5 0a.5.5 0 0 1 .5.5v6a.5.5 0 0 1-1 0V6a.5.5 0 0 1 .5-.5zm3 .5a.5.5 0 0 0-1 0v6a.5.5 0 0 0 1 0V6z"/><path fill="currentColor" fill-rule="evenodd" d="M14.5 3a1 1 0 0 1-1 1H13v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V4h-.5a1 1 0 0 1-1-1V2a1 1 0 0 1 1-1H6a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1h3.5a1 1 0 0 1 1 1v1zM4.118 4L4 4.059V13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V4.059L11.882 4H4.118zM2.5 3V2h11v1h-11z"/></svg>
            </button>
        </div>
    </div>

    <!-- Debug Toolbar (shown during debug sessions) -->
    <div id="debugToolbar" class="debug-toolbar" style="display: none;">
        <div class="debug-toolbar-section">
            <button class="debug-button debug-continue" onclick="debugContinue()" title="Continue (F5)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M3 2v12l10-6L3 2z"/></svg>
            </button>
            <button class="debug-button debug-step-over" onclick="debugStepOver()" title="Step Over (F10)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M14.25 5.75a4.25 4.25 0 0 0-8.5 0h2L4 10 .25 5.75h2a6.25 6.25 0 0 1 12.5 0h-.5z"/><circle cx="4" cy="13" r="2" fill="currentColor"/></svg>
            </button>
            <button class="debug-button debug-step-into" onclick="debugStepInto()" title="Step Into (F11)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M8 1v8.5L5.5 7 4 8.5l4 4 4-4L10.5 7 8 9.5V1H8z"/><circle cx="8" cy="14" r="2" fill="currentColor"/></svg>
            </button>
            <button class="debug-button debug-step-out" onclick="debugStepOut()" title="Step Out (Shift+F11)">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M8 15V6.5l2.5 2.5L12 7.5l-4-4-4 4L5.5 9 8 6.5V15h0z"/><circle cx="8" cy="2" r="2" fill="currentColor"/></svg>
            </button>
            <div class="debug-separator"></div>
            <button class="debug-button debug-stop" onclick="debugStop()" title="Stop (Shift+F5)">
                <svg width="16" height="16" viewBox="0 0 16 16"><rect x="3" y="3" width="10" height="10" fill="currentColor"/></svg>
            </button>
        </div>
        <div class="debug-status">
            <span id="debugStatus">Ready</span>
        </div>
    </div>

    <!-- Main IDE Layout -->
    <div class="ide-container">
        <!-- Activity Bar -->
        <div class="activity-bar">
            <div class="activity-item active" data-panel="explorer" title="Explorer">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M14.5 3H7.71l-.85-.85L6.51 2h-5a.5.5 0 0 0-.5.5v11a.5.5 0 0 0 .5.5h13a.5.5 0 0 0 .5-.5v-10a.5.5 0 0 0-.5-.5Z"/></svg>
            </div>
            <div class="activity-item" data-panel="search" title="Search">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="m15.7 13.3-3.81-3.83A5.93 5.93 0 0 0 13 6c0-3.31-2.69-6-6-6S1 2.69 1 6s2.69 6 6 6c1.3 0 2.48-.41 3.47-1.11l3.83 3.81c.19.2.45.3.7.3.25 0 .52-.09.7-.3a.996.996 0 0 0 0-1.4ZM7 10.7c-2.59 0-4.7-2.11-4.7-4.7 0-2.59 2.11-4.7 4.7-4.7 2.59 0 4.7 2.11 4.7 4.7 0 2.59-2.11 4.7-4.7 4.7Z"/></svg>
            </div>
            <div class="activity-item" data-panel="git" title="Source Control">
                <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M15.6 7.8L8.7.9c-.2-.2-.5-.2-.7 0L6.8 2.1 9.1 4.4c.2-.1.4-.1.6-.1.8 0 1.5.7 1.5 1.5 0 .2 0 .4-.1.6l2.2 2.2c.2-.1.4-.1.6-.1.8 0 1.5.7 1.5 1.5s-.7 1.5-1.5 1.5-1.5-.7-1.5-1.5c0-.2 0-.4.1-.6L9.3 7.2v4.3c.4.2.7.6.7 1.1 0 .8-.7 1.5-1.5 1.5s-1.5-.7-1.5-1.5c0-.5.3-.9.7-1.1V7.2c-.4-.2-.7-.6-.7-1.1 0-.2 0-.4.1-.6L4.9 3.3 1.1 7.1c-.2.2-.2.5 0 .7l6.9 6.9c.2.2.5.2.7 0l6.9-6.9c.2-.2.2-.5 0-.7Z"/></svg>
            </div>
        </div>
      <!-- Side Panel -->
        <div class="side-panel" id="sidePanel">
            <!-- Explorer Panel -->
            <div class="panel-content" id="explorer-panel">
                <div class="panel-header">
                    <span class="panel-title">EXPLORER</span>
                    <div class="panel-actions">
                        <button class="panel-action" onclick="createNewFile()" title="New File">
                            <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M14.5 13.5h-13a.5.5 0 0 1-.5-.5V3a.5.5 0 0 1 .5-.5h8.793l4.207 4.207v6.293a.5.5 0 0 1-.5.5ZM2 12h11V7.5L9.5 4H2v8Z"/><path fill="currentColor" d="M8 6V4h1v2h2v1H9v2H8V7H6V6h2Z"/></svg>
                        </button>
                        <button class="panel-action" onclick="refreshExplorer()" title="Refresh Explorer">
                            <svg width="16" height="16" viewBox="0 0 16 16"><path fill="currentColor" d="M8 3a5 5 0 1 0 4.546 2.914.5.5 0 0 1 .908-.418A6 6 0 1 1 8 2v1z"/><path fill="currentColor" d="M8 4.466V2.534a.25.25 0 0 1 .41-.192l2.36 1.966c.12.1.12.284 0 .384L8.41 6.658A.25.25 0 0 1 8 6.466V4.466z"/></svg>
                        </button>
                    </div>
                </div>
                <div class="file-tree" id="fileTree">
                    <!-- File tree will be populated here -->
                </div>
            </div>

            <!-- Search Panel -->
            <div class="panel-content hidden" id="search-panel">
                <div class="panel-header">
                    <span class="panel-title">SEARCH</span>
                </div>
                <div class="search-container">
                    <input type="text" id="searchInput" placeholder="Search files..." class="search-input">
                    <div class="search-results" id="searchResults"></div>
                </div>
            </div>
  

            <!-- Git Panel -->
            <div class="panel-content hidden" id="git-panel">
                <div class="panel-header">
                    <span class="panel-title">SOURCE CONTROL</span>
                </div>
                <div class="git-status">
                    <p>No repository detected</p>
                </div>
            </div>
            
            <!-- Resize Handle -->
            <div class="resize-handle" id="resizeHandle"></div>
        </div>

        <!-- Main Content Area -->
        <div class="main-content">
            <!-- Tab Bar -->
            <div class="tab-bar" id="tabBar">
                <!-- Tabs will be added dynamically -->
            </div>

            <!-- Editor Area -->
            <div class="editor-area">
                <div class="editor-group">
                    <div class="no-editor-message" id="noEditorMessage">
                        <div class="welcome-content">
                            <h2>GoLang Source File Viewer</h2>
                            <p>Viewing files from: ` + rootDirectory + `</p>
                            <p>Open a file to start editing</p>
                            <div class="quick-actions">
                                <button onclick="createNewFile()" class="quick-action">New File</button>
                                <button onclick="openFileDialog()" class="quick-action">Open File</button>
                            </div>
                        </div>
                    </div>
                    <div id="editorContainer" class="editor-container hidden">
                        <div id="monacoEditor" style="width: 100%; height: 100%;"></div>
                    </div>
                </div>
            </div>
        </div>

        <!-- Debug Panel - Variables and Call Stack (visible only during debug) -->
        <div id="debugPanel" class="debug-panel">
            <div class="debug-panel-resize" id="debugPanelResize"></div>
            <div class="debug-panel-header">
                <span class="debug-panel-title">Debug</span>
            </div>
            <div class="debug-panel-content">
                <!-- Variables Section -->
                <div class="debug-section" id="variablesSection">
                    <div class="debug-section-header" onclick="toggleDebugSection('variablesSection')">
                        <svg class="debug-section-chevron" viewBox="0 0 16 16">
                            <path fill="currentColor" d="M6 4l4 4-4 4V4z"/>
                        </svg>
                        <span class="debug-section-title">Variables</span>
                        <span class="debug-section-count" id="variablesCount">0</span>
                    </div>
                    <div class="debug-section-content" id="variablesContent">
                        <div class="debug-empty-state">No variables to display</div>
                    </div>
                </div>

                <!-- Call Stack Section -->
                <div class="debug-section" id="callStackSection">
                    <div class="debug-section-header" onclick="toggleDebugSection('callStackSection')">
                        <svg class="debug-section-chevron" viewBox="0 0 16 16">
                            <path fill="currentColor" d="M6 4l4 4-4 4V4z"/>
                        </svg>
                        <span class="debug-section-title">Call Stack</span>
                        <span class="debug-section-count" id="callStackCount">0</span>
                    </div>
                    <div class="debug-section-content" id="callStackContent">
                        <div class="debug-empty-state">No call stack to display</div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <!-- Terminal Panel -->
    <div class="terminal-panel" id="terminalPanel" style="display: none;">
        <!-- Terminal Resize Handle -->
        <div class="terminal-resize-handle" id="terminalResizeHandle"></div>
        <div class="terminal-header">
            <span class="terminal-title">TERMINAL</span>
            <div class="terminal-actions">
                <button class="terminal-action" onclick="clearTerminal()" title="Clear Terminal">
                    <svg width="14" height="14" viewBox="0 0 16 16"><path fill="currentColor" d="M8 2.5a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11zM3 8a5 5 0 1 1 10 0A5 5 0 0 1 3 8zm7.854-2.854a.5.5 0 0 1 0 .708L8.707 8l2.147 2.146a.5.5 0 0 1-.708.708L8 8.707l-2.146 2.147a.5.5 0 0 1-.708-.708L7.293 8 5.146 5.854a.5.5 0 1 1 .708-.708L8 7.293l2.146-2.147a.5.5 0 0 1 .708 0z"/></svg>
                </button>
                <button class="terminal-action" onclick="toggleTerminal()" title="Close Terminal">
                    <svg width="14" height="14" viewBox="0 0 16 16"><path fill="currentColor" d="M4.646 4.646a.5.5 0 0 1 .708 0L8 7.293l2.646-2.647a.5.5 0 0 1 .708.708L8.707 8l2.647 2.646a.5.5 0 0 1-.708.708L8 8.707l-2.646 2.647a.5.5 0 0 1-.708-.708L7.293 8 4.646 5.354a.5.5 0 0 1 0-.708z"/></svg>
                </button>
            </div>
        </div>
        <div class="terminal-content" id="terminalContent">
            <!-- Terminal output will be displayed here -->
        </div>
    </div>

    <!-- Status Bar -->
    <div class="status-bar">
        <div class="status-left">
            <span id="gitBranch" class="status-item hidden">
                <svg width="12" height="12" viewBox="0 0 16 16"><path fill="currentColor" d="M5.5 3.5a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM2 5.5a3.5 3.5 0 1 1 5.898 2.549 5.508 5.508 0 0 1 3.034 4.084.75.75 0 1 1-1.482.235 4 4 0 0 0-7.9 0 .75.75 0 0 1-1.482-.235A5.507 5.507 0 0 1 3.102 8.05 3.493 3.493 0 0 1 2 5.5z"/></svg>
                main
            </span>
            <span id="fileErrors" class="status-item hidden">
                <svg width="12" height="12" viewBox="0 0 16 16"><path fill="currentColor" d="M8.22 1.754a.25.25 0 0 0-.44 0L1.698 13.132a.25.25 0 0 0 .22.368h12.164a.25.25 0 0 0 .22-.368L8.22 1.754zm-1.763-.707c.659-1.234 2.427-1.234 3.086 0l6.082 11.378A1.75 1.75 0 0 1 14.082 15H1.918a1.75 1.75 0 0 1-1.543-2.575L6.457 1.047zM9 11a1 1 0 1 1-2 0 1 1 0 0 1 2 0zm-.25-5.25a.75.75 0 0 0-1.5 0v2.5a.75.75 0 0 0 1.5 0v-2.5z"/></svg>
                0
            </span>
            <span id="fileWarnings" class="status-item hidden">
                <svg width="12" height="12" viewBox="0 0 16 16"><path fill="currentColor" d="M6.457 1.047c.659-1.234 2.427-1.234 3.086 0l6.082 11.378A1.75 1.75 0 0 1 14.082 15H1.918a1.75 1.75 0 0 1-1.543-2.575L6.457 1.047zM8 5a.75.75 0 0 1 .75.75v2.5a.75.75 0 0 1-1.5 0v-2.5A.75.75 0 0 1 8 5zm1 6a1 1 0 1 1-2 0 1 1 0 0 1 2 0z"/></svg>
                0
            </span>
        </div>
        <div class="status-right">
            <span id="selectionInfo" class="status-item">Ln 1, Col 1</span>
            <span id="indentInfo" class="status-item">Spaces: 4</span>
            <span id="encodingInfo" class="status-item">UTF-8</span>
            <span id="fileType" class="status-item">Plain Text</span>
        </div>
    </div>

    <!-- Context Menu -->
    <div id="contextMenu" class="context-menu hidden">
        <div class="context-item" onclick="cutText()">Cut</div>
        <div class="context-item" onclick="copyText()">Copy</div>
        <div class="context-item" onclick="pasteText()">Paste</div>
        <div class="context-separator"></div>
        <div class="context-item" onclick="selectAllText()">Select All</div>
    </div>

    <!-- File Dialog Modal -->
    <div id="fileDialog" class="file-dialog-overlay hidden">
        <div class="file-dialog">
            <div class="file-dialog-header">
                <h3>Open File</h3>
                <button class="dialog-close" onclick="closeFileDialog()">×</button>
            </div>
            <div class="file-dialog-content">
                <div class="file-dialog-path">
                    <span id="currentPath">.</span>
                </div>
                <div class="file-dialog-list" id="fileDialogList">
                    <!-- Files will be populated here -->
                </div>
            </div>
            <div class="file-dialog-footer">
                <input type="text" id="selectedFileName" class="file-name-input" placeholder="Enter filename...">
                <div class="file-dialog-buttons">
                    <button onclick="closeFileDialog()" class="dialog-button dialog-button-cancel">Cancel</button>
                    <button onclick="openSelectedFile()" class="dialog-button dialog-button-primary">Open</button>
                </div>
            </div>
        </div>
    </div>

    <script src="/static/editor.js?v=` + fmt.Sprintf("%d", time.Now().Unix()) + `"></script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func openFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format")
		return
	}

	var fullPath string
	var err error

	// Check if this is an absolute path (e.g., from work directory)
	if filepath.IsAbs(req.Filename) {
		// For absolute paths, use them directly (allows opening temp files from workdir)
		fullPath = filepath.Clean(req.Filename)
		fmt.Printf("📂 Opening absolute path: %s\n", fullPath)
	} else {
		// Get the full path within the root directory
		fullPath, err = getFullPath(req.Filename)
		if err != nil {
			sendErrorResponse(w, "Invalid filename - path outside root directory")
			return
		}
		fmt.Printf("📂 Opening file: %s (full path: %s)\n", req.Filename, fullPath)
	}

	content, err := ioutil.ReadFile(fullPath)
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to read file: %v", err))
		return
	}

	response := FileResponse{
		Success: true,
		Content: string(content),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func saveFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format")
		return
	}

	// Get the full path within the root directory
	fullPath, err := getFullPath(req.Filename)
	if err != nil {
		sendErrorResponse(w, "Invalid filename - path outside root directory")
		return
	}

	// Log the file operation
	fmt.Printf("💾 Saving file: %s (%d bytes)\n", req.Filename, len(req.Content))

	// Create directory if it doesn't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to create directory: %v", err))
		return
	}

	if err := ioutil.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to write file: %v", err))
		return
	}

	response := FileResponse{Success: true}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// createHooksModule creates a new hooks module directory with go.mod and hooks file
func createHooksModule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DirName        string `json:"dirName"`
		ModuleName     string `json:"moduleName"`
		FileContent    string `json:"fileContent"`
		FileName       string `json:"fileName"`
		ForceOverwrite bool   `json:"forceOverwrite"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format")
		return
	}

	// Default directory name
	if req.DirName == "" {
		req.DirName = "generated_hooks"
	}

	// Get the full path within the root directory
	fullPath, err := getFullPath(req.DirName)
	if err != nil {
		sendErrorResponse(w, "Invalid directory name - path outside root directory")
		return
	}

	fmt.Printf("📁 Creating hooks module: %s\n", fullPath)

	// Create the directory
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to create directory: %v", err))
		return
	}

	// Default filename
	fileName := req.FileName
	if fileName == "" {
		fileName = "generated_hooks.go"
	}

	// Check if file already exists
	hooksFilePath := filepath.Join(fullPath, fileName)
	if _, err := os.Stat(hooksFilePath); err == nil {
		// File exists
		if !req.ForceOverwrite {
			// Return a response indicating file exists and needs confirmation
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    false,
				"fileExists": true,
				"filePath":   req.DirName + "/" + fileName,
				"message":    fmt.Sprintf("File '%s' already exists. Do you want to overwrite it?", req.DirName+"/"+fileName),
			})
			return
		}
		fmt.Printf("⚠️ Overwriting existing file: %s\n", hooksFilePath)
	}

	// Write the hooks file
	if err := ioutil.WriteFile(hooksFilePath, []byte(req.FileContent), 0644); err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to write hooks file: %v", err))
		return
	}
	fmt.Printf("📝 Created hooks file: %s\n", hooksFilePath)

	// Determine module name
	moduleName := req.ModuleName
	if moduleName == "" {
		// Try to detect parent module name from go.mod
		parentGoMod := filepath.Join(rootDirectory, "go.mod")
		if data, err := ioutil.ReadFile(parentGoMod); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "module ") {
					parentModule := strings.TrimPrefix(line, "module ")
					parentModule = strings.TrimSpace(parentModule)
					moduleName = parentModule + "/" + req.DirName
					break
				}
			}
		}
		// Fallback to directory name
		if moduleName == "" {
			moduleName = req.DirName
		}
	}

	// Run go mod init
	cmd := exec.Command("go", "mod", "init", moduleName)
	cmd.Dir = fullPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If go.mod already exists, that's okay
		if !strings.Contains(string(output), "go.mod already exists") {
			fmt.Printf("⚠️ go mod init warning: %s\n", string(output))
		}
	} else {
		fmt.Printf("✅ Created go.mod with module: %s\n", moduleName)
	}

	// Run go mod tidy to add dependencies in the hooks directory
	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = fullPath
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("⚠️ go mod tidy warning: %s\n", string(output))
	} else {
		fmt.Printf("✅ Updated hooks module dependencies\n")
	}

	// Return success with the created paths
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"directory":  req.DirName,
		"hooksFile":  req.DirName + "/generated_hooks.go",
		"moduleName": moduleName,
	})
}

func listFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "."
	}

	// Get the full path within the root directory
	fullPath, err := getFullPath(dir)
	if err != nil {
		sendErrorResponse(w, "Invalid directory - path outside root directory")
		return
	}

	files, err := ioutil.ReadDir(fullPath)
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to read directory: %v", err))
		return
	}

	var fileList []string

	// Add parent directory link
	// If restrictNavigation is disabled, allow navigating up to filesystem root
	// If restrictNavigation is enabled, only allow navigating within rootDirectory
	if !restrictNavigation {
		// Always show ".." unless we're at filesystem root "/"
		if fullPath != "/" {
			fileList = append(fileList, "../")
		}
	} else {
		// Only show ".." when we're not at the configured root directory
		if dir != "." && fullPath != rootDirectory {
			fileList = append(fileList, "../")
		}
	}

	// Add directories first, then files
	for _, file := range files {
		if file.IsDir() {
			fileList = append(fileList, file.Name()+"/")
		}
	}

	for _, file := range files {
		if !file.IsDir() {
			fileList = append(fileList, file.Name())
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"files":   fileList,
		"dir":     dir,
	})
}

func getPackFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Log the operation
	fmt.Printf("🔍 Executing pack-files command...\n")

	// Get absolute path to hc executable
	execPath, err := filepath.Abs("../hc/hc")
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to resolve executable path: %v", err))
		return
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Execute the external command with absolute path
	fmt.Printf("📍 Executing: %s --pack-files from directory: %s\n", execPath, rootDirectory)
	cmd := exec.Command(execPath, "--pack-files")
	cmd.Dir = rootDirectory // Set working directory to the root directory

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to execute hc: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
			err, execPath, rootDirectory, string(output))
		sendErrorResponse(w, errorMsg)
		return
	}

	// Return the command output
	response := FileResponse{
		Success: true,
		Content: string(output),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getPackFunctions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Log the operation
	fmt.Printf("⚙️ Executing pack-functions command...\n")

	// Get absolute path to hc executable
	execPath, err := filepath.Abs("../hc/hc")
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to resolve executable path: %v", err))
		return
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Execute the external command with absolute path
	fmt.Printf("📍 Executing: %s --pack-functions from directory: %s\n", execPath, rootDirectory)
	cmd := exec.Command(execPath, "--pack-functions")
	cmd.Dir = rootDirectory // Set working directory to the root directory

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to execute hc: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
			err, execPath, rootDirectory, string(output))
		sendErrorResponse(w, errorMsg)
		return
	}

	// Return the command output
	response := FileResponse{
		Success: true,
		Content: string(output),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getPackPackages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Log the operation
	fmt.Printf("📦 Executing pack-packages command...\n")

	// Get absolute path to hc executable
	execPath, err := filepath.Abs("../hc/hc")
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to resolve executable path: %v", err))
		return
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Execute the external command with absolute path
	fmt.Printf("📍 Executing: %s --pack-packages from directory: %s\n", execPath, rootDirectory)
	cmd := exec.Command(execPath, "--pack-packages")
	cmd.Dir = rootDirectory // Set working directory to the root directory

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to execute hc: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
			err, execPath, rootDirectory, string(output))
		sendErrorResponse(w, errorMsg)
		return
	}

	// Return the command output
	response := FileResponse{
		Success: true,
		Content: string(output),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getCallGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Log the operation
	fmt.Printf("🕸️ Executing callgraph command...\n")

	// Get absolute path to hc executable
	execPath, err := filepath.Abs("../hc/hc")
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to resolve executable path: %v", err))
		return
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Execute the external command with absolute path
	fmt.Printf("📍 Executing: %s --callgraph from directory: %s\n", execPath, rootDirectory)
	cmd := exec.Command(execPath, "--callgraph")
	cmd.Dir = rootDirectory // Set working directory to the root directory

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to execute hc: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
			err, execPath, rootDirectory, string(output))
		sendErrorResponse(w, errorMsg)
		return
	}

	// Return the command output
	response := FileResponse{
		Success: true,
		Content: string(output),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getWorkDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Log the operation
	fmt.Printf("📁 Executing workdir command...\n")

	// Get absolute path to hc executable
	execPath, err := filepath.Abs("../hc/hc")
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to resolve executable path: %v", err))
		return
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Execute the external command with absolute path
	fmt.Printf("📍 Executing: %s --workdir from directory: %s\n", execPath, rootDirectory)
	cmd := exec.Command(execPath, "--workdir")
	cmd.Dir = rootDirectory // Set working directory to the root directory

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to execute hc: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
			err, execPath, rootDirectory, string(output))
		sendErrorResponse(w, errorMsg)
		return
	}

	// Return the command output
	response := FileResponse{
		Success: true,
		Content: string(output),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		HooksFile string `json:"hooksFile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format")
		return
	}

	if req.HooksFile == "" {
		sendErrorResponse(w, "Hooks file is required for compile command")
		return
	}

	// Log the operation
	fmt.Printf("🔧 Executing compile command with hooks file: %s...\n", req.HooksFile)

	// Get absolute path to hc executable
	execPath, err := filepath.Abs("../hc/hc")
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to resolve executable path: %v", err))
		return
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Execute the external command with absolute path
	fmt.Printf("📍 Executing: %s --compile %s from directory: %s\n", execPath, req.HooksFile, rootDirectory)
	cmd := exec.Command(execPath, "--compile", req.HooksFile)
	cmd.Dir = rootDirectory // Set working directory to the root directory

	// Capture both stdout and stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to execute hc: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
			err, execPath, rootDirectory, string(output))
		sendErrorResponse(w, errorMsg)
		return
	}

	// Return the command output
	response := FileResponse{
		Success: true,
		Content: string(output),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getRunExecutable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ExecutablePath string `json:"executablePath"`
		Timeout        int    `json:"timeout"` // Timeout in seconds (default 10)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format")
		return
	}

	if req.ExecutablePath == "" {
		sendErrorResponse(w, "Executable path is required")
		return
	}

	// Default timeout of 10 seconds
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10
	}

	// Resolve the executable path relative to rootDirectory
	execPath := req.ExecutablePath
	if !filepath.IsAbs(execPath) {
		execPath = filepath.Join(rootDirectory, execPath)
	}

	// Log the operation
	fmt.Printf("🚀 Running executable: %s (timeout: %ds)\n", execPath, timeout)

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Execute the built program with a timeout context
	fmt.Printf("📍 Executing: %s from directory: %s\n", execPath, rootDirectory)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, execPath)
	cmd.Dir = rootDirectory

	// Use CombinedOutput in a goroutine to capture all output
	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)

	go func() {
		out, err := cmd.CombinedOutput()
		done <- result{output: out, err: err}
	}()

	// Wait for completion or timeout
	select {
	case res := <-done:
		output := string(res.output)
		if output == "" {
			output = "(no output)"
		}
		if res.err != nil {
			// Check if it was killed due to timeout
			if ctx.Err() == context.DeadlineExceeded {
				output = fmt.Sprintf("%s\n\n[Process killed after %d seconds timeout]", output, timeout)
			} else {
				output = fmt.Sprintf("%s\n\nProcess exited with: %v", output, res.err)
			}
		}
		response := FileResponse{
			Success: true,
			Content: output,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

	case <-ctx.Done():
		// Timeout occurred - the CommandContext will kill the process
		// Wait briefly for the goroutine to finish and collect any output
		select {
		case res := <-done:
			output := string(res.output)
			if output == "" {
				output = "(no output captured)"
			}
			output = fmt.Sprintf("%s\n\n[Process killed after %d seconds timeout]", output, timeout)
			response := FileResponse{
				Success: true,
				Content: output,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case <-time.After(1 * time.Second):
			// Process didn't respond to kill, return what we have
			response := FileResponse{
				Success: true,
				Content: fmt.Sprintf("[Process killed after %d seconds timeout - no output captured]", timeout),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	}
}

// SourceMapping represents a mapping from original source file to instrumented file
type SourceMapping struct {
	Original     string `json:"original"`
	Instrumented string `json:"instrumented"` // WORK directory path (what's in binary debug info)
	DebugCopy    string `json:"debugCopy"`    // Permanent copy for dlv to find
	DebugDir     string `json:"debugDir"`     // Base directory of debug copies
}

// SourceMappings contains all file mappings for dlv debugger
type SourceMappings struct {
	WorkDir  string          `json:"workDir"`
	Mappings []SourceMapping `json:"mappings"`
}

// Global dlv process management
var (
	dlvCmd   *exec.Cmd
	dlvMutex sync.Mutex
)

// Global running executable process management
var (
	runningCmd   *exec.Cmd
	runningMutex sync.Mutex
)

func handleDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ExecutablePath string `json:"executablePath"`
		Port           int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendErrorResponse(w, "Invalid request format")
		return
	}

	if req.ExecutablePath == "" {
		sendErrorResponse(w, "Executable path is required")
		return
	}

	// Default port
	if req.Port == 0 {
		req.Port = 2345
	}

	// Resolve the executable path relative to rootDirectory
	execPath := req.ExecutablePath
	if !filepath.IsAbs(execPath) {
		execPath = filepath.Join(rootDirectory, execPath)
	}

	// Log the operation
	fmt.Printf("🐛 Starting debug session for: %s\n", execPath)

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		sendErrorResponse(w, fmt.Sprintf("Executable not found at: %s", execPath))
		return
	}

	// Generate source mappings from existing build log
	fmt.Printf("📄 Generating source mappings...\n")
	interceptorPath, err := filepath.Abs("../hc/hc")
	if err == nil {
		if _, err := os.Stat(interceptorPath); err == nil {
			cmd := exec.Command(interceptorPath, "--source-mappings")
			cmd.Dir = rootDirectory
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("⚠️  Source mappings generation failed: %v\n%s\n", err, string(output))
			} else {
				fmt.Printf("✅ Source mappings generated\n")
			}
		}
	}

	// Read source mappings
	mappingsPath := filepath.Join(rootDirectory, "build-metadata", "source-mappings.json")
	var mappings SourceMappings
	var substitutePaths []string

	if data, err := ioutil.ReadFile(mappingsPath); err == nil {
		if err := json.Unmarshal(data, &mappings); err == nil {
			// Build substitute-path arguments
			for _, m := range mappings.Mappings {
				// Get the directory of the original file
				origDir := filepath.Dir(m.Original)
				// Get the directory of the instrumented file
				instrDir := filepath.Dir(m.Instrumented)
				// Add the substitute path (original -> instrumented)
				substitutePaths = append(substitutePaths, fmt.Sprintf("%s=%s", origDir, instrDir))
			}
			// Remove duplicates
			seen := make(map[string]bool)
			uniquePaths := []string{}
			for _, p := range substitutePaths {
				if !seen[p] {
					seen[p] = true
					uniquePaths = append(uniquePaths, p)
				}
			}
			substitutePaths = uniquePaths
		}
	}

	// Kill any existing dlv process
	dlvMutex.Lock()
	if dlvCmd != nil && dlvCmd.Process != nil {
		dlvCmd.Process.Kill()
		dlvCmd.Wait()
		dlvCmd = nil
	}
	dlvMutex.Unlock()

	// Build dlv command (substitute-path is configured via JSON-RPC, not CLI)
	args := []string{
		"exec",
		execPath,
		"--headless",
		fmt.Sprintf("--listen=:%d", req.Port),
		"--api-version=2",
		"--accept-multiclient",
	}

	fmt.Printf("📍 Executing: dlv %s\n", strings.Join(args, " "))
	if len(substitutePaths) > 0 {
		fmt.Printf("📍 Source mappings (for reference): %v\n", substitutePaths)
	}

	cmd := exec.Command("dlv", args...)
	cmd.Dir = rootDirectory

	// Capture stderr to see dlv errors
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to create stderr pipe: %v", err))
		return
	}

	// Start dlv in background
	if err := cmd.Start(); err != nil {
		sendErrorResponse(w, fmt.Sprintf("Failed to start dlv: %v", err))
		return
	}

	dlvMutex.Lock()
	dlvCmd = cmd
	dlvMutex.Unlock()

	// Read initial stderr output in a goroutine
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				log.Printf("[dlv stderr] %s", string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for dlv to start and verify it's listening
	var dlvReady bool
	for i := 0; i < 20; i++ { // Try for up to 2 seconds
		time.Sleep(100 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", req.Port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			dlvReady = true
			break
		}
	}

	if !dlvReady {
		// Check if process is still running
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		sendErrorResponse(w, "dlv started but is not responding on the specified port")
		return
	}

	log.Printf("dlv is ready and listening on port %d\n", req.Port)

	// Build response with connection info
	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Delve debugger started on port %d", req.Port),
		"port":    req.Port,
		"pid":     cmd.Process.Pid,
		"substitutePaths": substitutePaths,
		"connectCommand":  fmt.Sprintf("dlv connect :%d", req.Port),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DebugCommand represents a command from the browser
type DebugCommand struct {
	Command string `json:"command"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	ID      int    `json:"id,omitempty"`
}

// DlvRequest represents a JSON-RPC request to dlv
type DlvRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	ID     int           `json:"id"`
}

// DlvResponse represents a JSON-RPC response from dlv
type DlvResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  interface{}     `json:"error,omitempty"`
}

var dlvRequestID = 0
var dlvRequestMethods = make(map[int]string) // Track request ID -> method name
var dlvRequestMutex sync.Mutex               // Protects dlvRequestID and dlvRequestMethods

// translateFilePaths recursively translates file paths in a JSON structure
// from instrumented paths back to original paths
func translateFilePaths(v interface{}, mapping map[string]string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, vv := range val {
			if k == "file" || k == "File" {
				if fileStr, ok := vv.(string); ok {
					if origFile, found := mapping[fileStr]; found {
						log.Printf("Translating file path: %s -> %s\n", fileStr, origFile)
						result[k] = origFile
					} else {
						result[k] = vv
					}
				} else {
					result[k] = vv
				}
			} else {
				result[k] = translateFilePaths(vv, mapping)
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, vv := range val {
			result[i] = translateFilePaths(vv, mapping)
		}
		return result
	default:
		return v
	}
}

func handleDebugWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Debug WebSocket upgrade failed: %v\n", err)
		return
	}
	defer conn.Close()

	// Get port from query params
	portStr := r.URL.Query().Get("port")
	port := 2345
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	// Load source mappings for file path translation
	mappingsPath := filepath.Join(rootDirectory, "build-metadata", "source-mappings.json")
	origToInstr := make(map[string]string) // original -> instrumented (WORK dir path)
	instrToOrig := make(map[string]string) // instrumented -> original
	var substitutePaths []struct{ From, To string }

	if data, err := ioutil.ReadFile(mappingsPath); err == nil {
		var mappings SourceMappings
		if err := json.Unmarshal(data, &mappings); err == nil {
			for _, m := range mappings.Mappings {
				origToInstr[m.Original] = m.Instrumented
				instrToOrig[m.Instrumented] = m.Original
				// Build substitute path: original dir -> instrumented dir
				origDir := filepath.Dir(m.Original)
				instrDir := filepath.Dir(m.Instrumented)
				substitutePaths = append(substitutePaths, struct{ From, To string }{origDir, instrDir})
				log.Printf("Source mapping: %s -> %s\n", m.Original, m.Instrumented)
			}
		}
	}

	log.Printf("Debug WebSocket connection established, connecting to dlv on port %d\n", port)

	// Connect to dlv
	dlvConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		log.Printf("Failed to connect to dlv: %v\n", err)
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": fmt.Sprintf("Failed to connect to dlv: %v", err),
		})
		return
	}
	defer dlvConn.Close()

	// Configure dlv substitute-path to map original files to instrumented files
	if len(substitutePaths) > 0 {
		// Build the Dir array for dlv
		dirRules := make([]map[string]string, len(substitutePaths))
		for i, sp := range substitutePaths {
			dirRules[i] = map[string]string{"From": sp.From, "To": sp.To}
			log.Printf("Configuring dlv substitute-path: %s -> %s\n", sp.From, sp.To)
		}

		configReq := DlvRequest{
			ID:     999,
			Method: "RPCServer.SetApiVersion",
			Params: []interface{}{map[string]interface{}{"APIVersion": 2}},
		}
		configBytes, _ := json.Marshal(configReq)
		configBytes = append(configBytes, '\n')
		dlvConn.Write(configBytes)

		// Read response (discard it)
		reader := bufio.NewReader(dlvConn)
		reader.ReadBytes('\n')

		// Now set substitute path (dlv expects Dir array with From/To)
		substituteReq := DlvRequest{
			ID:     998,
			Method: "RPCServer.SetSubstitutePath",
			Params: []interface{}{
				map[string]interface{}{
					"Dir": dirRules,
				},
			},
		}
		subBytes, _ := json.Marshal(substituteReq)
		subBytes = append(subBytes, '\n')
		dlvConn.Write(subBytes)
		log.Printf("Sent substitute path config to dlv\n")

		// Read response
		resp, _ := reader.ReadBytes('\n')
		log.Printf("Substitute path response: %s\n", string(resp))
	}

	// Create a channel for dlv responses
	dlvResponses := make(chan []byte, 10)
	done := make(chan struct{})
	defer close(done)

	// Goroutine to read from dlv
	go func() {
		reader := bufio.NewReader(dlvConn)
		for {
			select {
			case <-done:
				return
			default:
				line, err := reader.ReadBytes('\n')
				if err != nil {
					if err != io.EOF {
						log.Printf("Error reading from dlv: %v\n", err)
					}
					return
				}
				dlvResponses <- line
			}
		}
	}()

	// Goroutine to forward dlv responses to browser
	go func() {
		for {
			select {
			case <-done:
				return
			case response := <-dlvResponses:
				log.Printf("Raw dlv response: %s\n", string(response))

				var dlvResp DlvResponse
				if err := json.Unmarshal(response, &dlvResp); err != nil {
					log.Printf("Error parsing dlv response: %v\n", err)
					continue
				}

				// Translate file paths in the result from instrumented to original
				var result interface{}
				if len(dlvResp.Result) > 0 {
					if err := json.Unmarshal(dlvResp.Result, &result); err == nil {
						result = translateFilePaths(result, instrToOrig)
					} else {
						result = dlvResp.Result
					}
				}

				log.Printf("Parsed dlv response - ID: %d, Error: %v\n", dlvResp.ID, dlvResp.Error)

				// Get the method name for this request ID
				dlvRequestMutex.Lock()
				method := dlvRequestMethods[dlvResp.ID]
				delete(dlvRequestMethods, dlvResp.ID) // Clean up
				dlvRequestMutex.Unlock()

				// Forward to browser
				conn.WriteJSON(map[string]interface{}{
					"type":   "response",
					"id":     dlvResp.ID,
					"method": method,
					"result": result,
					"error":  dlvResp.Error,
				})
			}
		}
	}()

	// Read commands from browser
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Debug WebSocket error: %v\n", err)
			}
			break
		}

		var cmd DebugCommand
		if err := json.Unmarshal(message, &cmd); err != nil {
			log.Printf("Error parsing debug command: %v\n", err)
			continue
		}

		log.Printf("Debug command: %s\n", cmd.Command)

		// Translate command to dlv JSON-RPC
		var dlvReq DlvRequest
		dlvRequestID++
		dlvReq.ID = dlvRequestID

		switch cmd.Command {
		case "continue":
			dlvReq.Method = "RPCServer.Command"
			dlvReq.Params = []interface{}{map[string]interface{}{"name": "continue"}}
		case "next":
			dlvReq.Method = "RPCServer.Command"
			dlvReq.Params = []interface{}{map[string]interface{}{"name": "next"}}
		case "step":
			dlvReq.Method = "RPCServer.Command"
			dlvReq.Params = []interface{}{map[string]interface{}{"name": "step"}}
		case "stepOut":
			dlvReq.Method = "RPCServer.Command"
			dlvReq.Params = []interface{}{map[string]interface{}{"name": "stepOut"}}
		case "halt":
			dlvReq.Method = "RPCServer.Command"
			dlvReq.Params = []interface{}{map[string]interface{}{"name": "halt"}}
		case "setBreakpoint":
			dlvReq.Method = "RPCServer.CreateBreakpoint"
			// Translate original file path to instrumented file path
			bpFile := cmd.File
			if instrFile, ok := origToInstr[cmd.File]; ok {
				log.Printf("Translating breakpoint file: %s -> %s\n", cmd.File, instrFile)
				bpFile = instrFile
			} else {
				log.Printf("No mapping found for file: %s (using as-is)\n", cmd.File)
			}
			dlvReq.Params = []interface{}{map[string]interface{}{
				"Breakpoint": map[string]interface{}{
					"file": bpFile,
					"line": cmd.Line,
				},
			}}
		case "clearBreakpoint":
			dlvReq.Method = "RPCServer.ClearBreakpoint"
			dlvReq.Params = []interface{}{map[string]interface{}{
				"Id": cmd.ID,
			}}
		case "state":
			dlvReq.Method = "RPCServer.State"
			dlvReq.Params = []interface{}{map[string]interface{}{}}
		case "listLocalVars":
			// List local variables in current scope
			dlvReq.Method = "RPCServer.ListLocalVars"
			dlvReq.Params = []interface{}{
				map[string]interface{}{
					"Scope": map[string]interface{}{
						"GoroutineID": -1, // Current goroutine
						"Frame":       0,  // Current frame
					},
					"Cfg": map[string]interface{}{
						"FollowPointers":     true,
						"MaxVariableRecurse": 1,
						"MaxStringLen":       64,
						"MaxArrayValues":     64,
						"MaxStructFields":    -1,
					},
				},
			}
		case "listFunctionArgs":
			// List function arguments
			dlvReq.Method = "RPCServer.ListFunctionArgs"
			dlvReq.Params = []interface{}{
				map[string]interface{}{
					"Scope": map[string]interface{}{
						"GoroutineID": -1,
						"Frame":       0,
					},
					"Cfg": map[string]interface{}{
						"FollowPointers":     true,
						"MaxVariableRecurse": 1,
						"MaxStringLen":       64,
						"MaxArrayValues":     64,
						"MaxStructFields":    -1,
					},
				},
			}
		case "stacktrace":
			// Get call stack
			dlvReq.Method = "RPCServer.Stacktrace"
			dlvReq.Params = []interface{}{
				map[string]interface{}{
					"Id":    -1, // Current goroutine
					"Depth": 50, // Max stack depth
					"Full":  false,
					"Cfg": map[string]interface{}{
						"FollowPointers":     false,
						"MaxVariableRecurse": 0,
						"MaxStringLen":       64,
						"MaxArrayValues":     0,
						"MaxStructFields":    0,
					},
				},
			}
		case "stop":
			// Detach from the process
			dlvReq.Method = "RPCServer.Detach"
			dlvReq.Params = []interface{}{map[string]interface{}{"Kill": true}}
		default:
			log.Printf("Unknown debug command: %s\n", cmd.Command)
			continue
		}

		// Track the method for this request ID
		dlvRequestMutex.Lock()
		dlvRequestMethods[dlvReq.ID] = dlvReq.Method
		dlvRequestMutex.Unlock()

		// Send to dlv
		reqBytes, _ := json.Marshal(dlvReq)
		reqBytes = append(reqBytes, '\n')
		if _, err := dlvConn.Write(reqBytes); err != nil {
			log.Printf("Error writing to dlv: %v\n", err)
			break
		}
	}
}

func sendErrorResponse(w http.ResponseWriter, message string) {
	fmt.Printf("Error: %s\n", message)
	response := FileResponse{
		Success: false,
		Error:   message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(response)
}

// handleCleanup removes build-metadata and .debug-build directories
func handleCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("🧹 Cleaning build artifacts...\n")

	var deletedDirs []string
	var errors []string

	// Directories to clean
	dirsToClean := []string{"build-metadata", ".debug-build"}

	for _, dir := range dirsToClean {
		dirPath := filepath.Join(rootDirectory, dir)
		if _, err := os.Stat(dirPath); err == nil {
			// Directory exists, remove it
			if err := os.RemoveAll(dirPath); err != nil {
				errors = append(errors, fmt.Sprintf("Failed to remove %s: %v", dir, err))
				fmt.Printf("❌ Failed to remove %s: %v\n", dir, err)
			} else {
				deletedDirs = append(deletedDirs, dir)
				fmt.Printf("✅ Removed %s\n", dir)
			}
		} else {
			fmt.Printf("ℹ️ %s does not exist, skipping\n", dir)
		}
	}

	// Build response message
	var message string
	if len(deletedDirs) > 0 {
		message = fmt.Sprintf("Cleaned: %s", strings.Join(deletedDirs, ", "))
	} else {
		message = "No build artifacts to clean"
	}

	if len(errors) > 0 {
		message += "\nErrors: " + strings.Join(errors, "; ")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     len(errors) == 0,
		"message":     message,
		"deletedDirs": deletedDirs,
		"errors":      errors,
	})
}

// handleRunWebSocket handles WebSocket connections for running executables with real-time output
func handleRunWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Run WebSocket upgrade failed: %v\n", err)
		return
	}
	defer conn.Close()

	log.Println("Run WebSocket connection established")

	// Read the initial command to start the process
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Printf("Error reading initial message: %v\n", err)
		return
	}

	var req struct {
		Command        string `json:"command"`
		ExecutablePath string `json:"executablePath"`
	}
	if err := json.Unmarshal(message, &req); err != nil {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Invalid request format",
		})
		return
	}

	if req.Command != "start" || req.ExecutablePath == "" {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Invalid command or missing executable path",
		})
		return
	}

	// Resolve the executable path relative to rootDirectory
	execPath := req.ExecutablePath
	if !filepath.IsAbs(execPath) {
		execPath = filepath.Join(rootDirectory, execPath)
	}

	// Check if executable exists
	if _, err := os.Stat(execPath); os.IsNotExist(err) {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": fmt.Sprintf("Executable not found: %s", execPath),
		})
		return
	}

	// Kill any existing running process
	runningMutex.Lock()
	if runningCmd != nil && runningCmd.Process != nil {
		log.Printf("Killing existing process (PID: %d)\n", runningCmd.Process.Pid)
		runningCmd.Process.Kill()
		runningCmd.Wait()
		runningCmd = nil
	}
	runningMutex.Unlock()

	// Create the command
	cmd := exec.Command(execPath)
	cmd.Dir = rootDirectory

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": fmt.Sprintf("Failed to create stdout pipe: %v", err),
		})
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": fmt.Sprintf("Failed to create stderr pipe: %v", err),
		})
		return
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": fmt.Sprintf("Failed to start process: %v", err),
		})
		return
	}

	// Store the running command
	runningMutex.Lock()
	runningCmd = cmd
	runningMutex.Unlock()

	log.Printf("Started process: %s (PID: %d)\n", execPath, cmd.Process.Pid)

	// Send started message
	conn.WriteJSON(map[string]interface{}{
		"type": "started",
		"pid":  cmd.Process.Pid,
	})

	// Channels for coordination
	done := make(chan struct{})
	processExited := make(chan error, 1)
	clientCommand := make(chan string, 1)
	clientDisconnected := make(chan struct{})

	// Mutex for safe WebSocket writes from multiple goroutines
	var connMutex sync.Mutex
	safeWriteJSON := func(v interface{}) error {
		connMutex.Lock()
		defer connMutex.Unlock()
		return conn.WriteJSON(v)
	}

	// Goroutine to read stdout and send to WebSocket
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			select {
			case <-done:
				return
			default:
				safeWriteJSON(map[string]interface{}{
					"type":   "stdout",
					"output": line,
				})
			}
		}
	}()

	// Goroutine to read stderr and send to WebSocket
	go func() {
		reader := bufio.NewReader(stderr)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			select {
			case <-done:
				return
			default:
				safeWriteJSON(map[string]interface{}{
					"type":   "stderr",
					"output": line,
				})
			}
		}
	}()

	// Goroutine to wait for process to exit
	go func() {
		err := cmd.Wait()
		processExited <- err
	}()

	// Goroutine to read commands from WebSocket (blocking read in separate goroutine)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				// Client disconnected or error
				close(clientDisconnected)
				return
			}

			var req struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(message, &req); err == nil {
				select {
				case clientCommand <- req.Command:
				case <-done:
					return
				}
			}
		}
	}()

	// Main event loop - wait for process exit, stop command, or client disconnect
	select {
	case err := <-processExited:
		close(done)
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		safeWriteJSON(map[string]interface{}{
			"type":     "exited",
			"exitCode": exitCode,
		})
		log.Printf("Process exited with code: %d\n", exitCode)

		runningMutex.Lock()
		runningCmd = nil
		runningMutex.Unlock()

	case cmd := <-clientCommand:
		if cmd == "stop" {
			log.Println("Received stop command")
			close(done)
			if runningCmd != nil && runningCmd.Process != nil {
				runningCmd.Process.Kill()
			}
			safeWriteJSON(map[string]interface{}{
				"type":    "stopped",
				"message": "Process killed by user",
			})
			runningMutex.Lock()
			runningCmd = nil
			runningMutex.Unlock()
		}

	case <-clientDisconnected:
		log.Println("Client disconnected, killing process")
		close(done)
		if runningCmd != nil && runningCmd.Process != nil {
			runningCmd.Process.Kill()
		}
		runningMutex.Lock()
		runningCmd = nil
		runningMutex.Unlock()
	}
}

// handleStopProcess stops the currently running process
func handleStopProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	runningMutex.Lock()
	defer runningMutex.Unlock()

	if runningCmd == nil || runningCmd.Process == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "No process is currently running",
		})
		return
	}

	pid := runningCmd.Process.Pid
	if err := runningCmd.Process.Kill(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Failed to kill process: %v", err),
		})
		return
	}

	runningCmd = nil
	log.Printf("Killed process (PID: %d)\n", pid)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Process %d killed", pid),
	})
}
