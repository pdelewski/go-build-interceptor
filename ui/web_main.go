package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

func main() {
	// Parse command line flags
	flag.StringVar(&rootDirectory, "dir", ".", "Root directory to serve files from")
	port := flag.String("port", "9090", "Port to serve on")
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

	// Ensure the path is within the root directory
	if !strings.HasPrefix(fullPath, rootDirectory) {
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
    <!-- Prism.js for syntax highlighting (local) -->
    <link href="/static/prism-tomorrow.min.css" rel="stylesheet">
    <script src="/static/prism.min.js"></script>
    <script src="/static/prism-go.min.js"></script>
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
                        <pre id="syntaxHighlight" class="syntax-highlight"><code id="highlightedCode" class="language-go"></code></pre>
                        <textarea id="editor" class="editor-textarea" placeholder="// Start coding..." spellcheck="false"></textarea>
                    </div>
                </div>
            </div>
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

	// Get the full path within the root directory
	fullPath, err := getFullPath(req.Filename)
	if err != nil {
		sendErrorResponse(w, "Invalid filename - path outside root directory")
		return
	}

	// Log the file operation
	fmt.Printf("📂 Opening file: %s (full path: %s)\n", req.Filename, fullPath)

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

	// Add parent directory link if not in root
	if dir != "." && fullPath != rootDirectory {
		fileList = append(fileList, "../")
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

	// Get absolute path to go-build-interceptor executable
	execPath, err := filepath.Abs("../go-build-interceptor")
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
		errorMsg := fmt.Sprintf("Failed to execute go-build-interceptor: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
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

	// Get absolute path to go-build-interceptor executable
	execPath, err := filepath.Abs("../go-build-interceptor")
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
		errorMsg := fmt.Sprintf("Failed to execute go-build-interceptor: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
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

	// Get absolute path to go-build-interceptor executable
	execPath, err := filepath.Abs("../go-build-interceptor")
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
		errorMsg := fmt.Sprintf("Failed to execute go-build-interceptor: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
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

	// Get absolute path to go-build-interceptor executable
	execPath, err := filepath.Abs("../go-build-interceptor")
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
		errorMsg := fmt.Sprintf("Failed to execute go-build-interceptor: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
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

	// Get absolute path to go-build-interceptor executable
	execPath, err := filepath.Abs("../go-build-interceptor")
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
		errorMsg := fmt.Sprintf("Failed to execute go-build-interceptor: %v\nExecutable: %s\nWorking Dir: %s\nOutput: %s",
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
