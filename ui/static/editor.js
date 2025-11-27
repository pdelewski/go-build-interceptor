// VS Code-like IDE JavaScript
class CodeEditor {
    constructor() {
        this.openTabs = new Map(); // Map of filename -> tab content
        this.activeTab = null;
        this.currentContent = '';
        this.isModified = false;
        this.activeSidePanel = 'explorer';
        
        // DOM elements
        this.editor = document.getElementById('editor');
        this.editorContainer = document.getElementById('editorContainer');
        this.syntaxHighlight = document.getElementById('syntaxHighlight');
        this.highlightedCode = document.getElementById('highlightedCode');
        this.tabBar = document.getElementById('tabBar');
        this.fileTree = document.getElementById('fileTree');
        this.noEditorMessage = document.getElementById('noEditorMessage');
        this.statusBar = {
            selection: document.getElementById('selectionInfo'),
            indent: document.getElementById('indentInfo'),
            encoding: document.getElementById('encodingInfo'),
            fileType: document.getElementById('fileType'),
            errors: document.getElementById('fileErrors'),
            warnings: document.getElementById('fileWarnings'),
            gitBranch: document.getElementById('gitBranch')
        };
        
        this.initializeEventListeners();
        this.loadFileTree();
        this.updateUI();
    }
    
    initializeEventListeners() {
        // Activity bar clicks
        document.querySelectorAll('.activity-item').forEach(item => {
            item.addEventListener('click', (e) => {
                const panel = item.dataset.panel;
                this.switchSidePanel(panel);
            });
        });
        
        // Menu bar functionality
        this.setupMenus();
        
        // Editor events
        this.editor.addEventListener('input', () => this.onEditorChange());
        this.editor.addEventListener('scroll', () => {
            this.syncScroll();
            this.updateStatusBar();
        });
        this.editor.addEventListener('keydown', (e) => this.onKeyDown(e));
        this.editor.addEventListener('keyup', () => this.updateStatusBar());
        this.editor.addEventListener('click', () => this.updateStatusBar());
        
        // Context menu
        this.editor.addEventListener('contextmenu', (e) => this.showContextMenu(e));
        document.addEventListener('click', (e) => {
            this.hideContextMenu();
            this.hideAllMenus();
        });
        
        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => this.handleGlobalShortcuts(e));
        
        // Window events
        window.addEventListener('beforeunload', (e) => this.onBeforeUnload(e));
        
        // File tree refresh
        document.getElementById('refreshBtn')?.addEventListener('click', () => this.loadFileTree());
    }
    
    setupMenus() {
        document.querySelectorAll('.menu-item').forEach(menuItem => {
            menuItem.addEventListener('click', (e) => {
                e.stopPropagation();
                
                // Close all menus first
                this.hideAllMenus();
                
                // Toggle current menu
                const dropdown = menuItem.querySelector('.dropdown-menu');
                if (dropdown) {
                    dropdown.classList.toggle('show');
                    menuItem.classList.toggle('active');
                }
            });
        });
        
        // Prevent menu close when clicking inside dropdown
        document.querySelectorAll('.dropdown-menu').forEach(menu => {
            menu.addEventListener('click', (e) => {
                e.stopPropagation();
            });
        });
        
        // Close menu when clicking menu option
        document.querySelectorAll('.menu-option').forEach(option => {
            option.addEventListener('click', () => {
                this.hideAllMenus();
            });
        });
    }
    
    hideAllMenus() {
        document.querySelectorAll('.dropdown-menu').forEach(menu => {
            menu.classList.remove('show');
        });
        document.querySelectorAll('.menu-item').forEach(item => {
            item.classList.remove('active');
        });
    }
    
    switchSidePanel(panelName) {
        // Update activity bar
        document.querySelectorAll('.activity-item').forEach(item => {
            item.classList.remove('active');
        });
        document.querySelector(`[data-panel="${panelName}"]`).classList.add('active');
        
        // Update panels
        document.querySelectorAll('.panel-content').forEach(panel => {
            panel.classList.add('hidden');
        });
        document.getElementById(`${panelName}-panel`).classList.remove('hidden');
        
        this.activeSidePanel = panelName;
        
        // Load content based on panel
        if (panelName === 'explorer') {
            this.loadFileTree();
        } else if (panelName === 'search') {
            document.getElementById('searchInput')?.focus();
        }
    }
    
    async loadFileTree() {
        try {
            const response = await fetch('/api/list');
            const result = await response.json();
            
            if (result.success) {
                this.populateFileTree(result.files);
            }
        } catch (error) {
            console.error('Error loading file tree:', error);
        }
    }
    
    populateFileTree(files) {
        this.fileTree.innerHTML = '';
        
        files.forEach(file => {
            const fileItem = document.createElement('div');
            fileItem.className = 'file-item';
            
            const icon = document.createElement('span');
            icon.className = 'file-icon';
            
            if (file.endsWith('/')) {
                fileItem.classList.add('directory');
                icon.classList.add('folder');
                fileItem.textContent = file.slice(0, -1);
            } else {
                const ext = file.split('.').pop()?.toLowerCase() || 'txt';
                icon.classList.add(ext);
                fileItem.textContent = file;
                
                // Make files clickable
                fileItem.addEventListener('click', () => this.openFile(file));
                fileItem.addEventListener('dblclick', () => this.openFile(file));
            }
            
            fileItem.insertBefore(icon, fileItem.firstChild);
            this.fileTree.appendChild(fileItem);
        });
    }
    
    async openFile(filename) {
        try {
            // Show loading state
            this.setStatus('Loading...', 'info');
            
            const response = await fetch('/api/open', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ filename })
            });
            
            const result = await response.json();
            
            if (result.success) {
                this.createOrSwitchTab(filename, result.content);
                this.setStatus(`Opened ${filename}`, 'success');
            } else {
                this.setStatus(`Error: ${result.error}`, 'error');
            }
        } catch (error) {
            this.setStatus(`Error opening file: ${error.message}`, 'error');
        }
    }
    
    createOrSwitchTab(filename, content) {
        // Check if tab already exists
        if (this.openTabs.has(filename)) {
            this.switchTab(filename);
            return;
        }
        
        // Create new tab
        const tab = document.createElement('div');
        tab.className = 'tab';
        tab.dataset.filename = filename;
        
        const tabName = document.createElement('span');
        tabName.textContent = filename.split('/').pop();
        tab.appendChild(tabName);
        
        const closeBtn = document.createElement('span');
        closeBtn.className = 'tab-close';
        closeBtn.innerHTML = '×';
        closeBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            this.closeTab(filename);
        });
        tab.appendChild(closeBtn);
        
        tab.addEventListener('click', () => this.switchTab(filename));
        
        this.tabBar.appendChild(tab);
        
        // Store tab content
        this.openTabs.set(filename, {
            content,
            originalContent: content,
            modified: false
        });
        
        this.switchTab(filename);
        this.updateUI();
    }
    
    switchTab(filename) {
        // Save current tab state
        if (this.activeTab) {
            const tabData = this.openTabs.get(this.activeTab);
            if (tabData) {
                tabData.content = this.editor.value;
                tabData.modified = tabData.content !== tabData.originalContent;
            }
        }
        
        // Update UI
        document.querySelectorAll('.tab').forEach(tab => {
            tab.classList.remove('active');
            if (tab.dataset.filename === filename) {
                tab.classList.add('active');
                if (this.openTabs.get(filename)?.modified) {
                    tab.classList.add('modified');
                } else {
                    tab.classList.remove('modified');
                }
            }
        });
        
        // Load new tab content
        const tabData = this.openTabs.get(filename);
        if (tabData) {
            this.editor.value = tabData.content;
            this.activeTab = filename;
            this.currentContent = tabData.content;
            this.isModified = tabData.modified;
        }
        
        this.updateUI();
        this.updateStatusBar();
        this.editor.focus();
    }
    
    closeTab(filename) {
        const tabData = this.openTabs.get(filename);
        
        if (tabData?.modified) {
            if (!confirm(`'${filename}' has unsaved changes. Close anyway?`)) {
                return;
            }
        }
        
        // Remove tab from DOM
        const tab = document.querySelector(`[data-filename="${filename}"]`);
        if (tab) {
            tab.remove();
        }
        
        // Remove from open tabs
        this.openTabs.delete(filename);
        
        // Switch to another tab or show welcome screen
        if (this.activeTab === filename) {
            const remainingTabs = Array.from(this.openTabs.keys());
            if (remainingTabs.length > 0) {
                this.switchTab(remainingTabs[remainingTabs.length - 1]);
            } else {
                this.activeTab = null;
                this.editor.value = '';
                this.updateUI();
            }
        }
    }
    
    onEditorChange() {
        if (this.activeTab) {
            const tabData = this.openTabs.get(this.activeTab);
            if (tabData) {
                tabData.content = this.editor.value;
                tabData.modified = tabData.content !== tabData.originalContent;
                
                // Update tab visual state
                const tab = document.querySelector(`[data-filename="${this.activeTab}"]`);
                if (tab) {
                    if (tabData.modified) {
                        tab.classList.add('modified');
                    } else {
                        tab.classList.remove('modified');
                    }
                }
            }
        }
        
        // Update syntax highlighting for Go files
        this.updateSyntaxHighlighting();
        this.updateStatusBar();
    }
    
    onKeyDown(e) {
        // Tab handling
        if (e.key === 'Tab') {
            e.preventDefault();
            const start = this.editor.selectionStart;
            const end = this.editor.selectionEnd;
            
            if (e.shiftKey) {
                // Remove indentation
                const lines = this.editor.value.split('\n');
                const startLine = this.editor.value.substring(0, start).split('\n').length - 1;
                const endLine = this.editor.value.substring(0, end).split('\n').length - 1;
                
                for (let i = startLine; i <= endLine; i++) {
                    if (lines[i].startsWith('    ')) {
                        lines[i] = lines[i].substring(4);
                    } else if (lines[i].startsWith('\t')) {
                        lines[i] = lines[i].substring(1);
                    }
                }
                
                this.editor.value = lines.join('\n');
            } else {
                // Add indentation
                this.editor.value = 
                    this.editor.value.substring(0, start) + 
                    '    ' + 
                    this.editor.value.substring(end);
                
                this.editor.selectionStart = this.editor.selectionEnd = start + 4;
            }
            
            this.onEditorChange();
        }
    }
    
    handleGlobalShortcuts(e) {
        if (e.ctrlKey || e.metaKey) {
            switch (e.key) {
                case 's':
                    e.preventDefault();
                    this.saveCurrentFile();
                    break;
                case 'n':
                    e.preventDefault();
                    this.createNewFile();
                    break;
                case 'o':
                    e.preventDefault();
                    this.showOpenDialog();
                    break;
                case 'w':
                    e.preventDefault();
                    if (this.activeTab) {
                        this.closeTab(this.activeTab);
                    }
                    break;
                case 'f':
                    e.preventDefault();
                    this.switchSidePanel('search');
                    break;
            }
        }
    }
    
    async saveCurrentFile() {
        if (!this.activeTab) {
            this.setStatus('No file to save', 'warning');
            return;
        }
        
        try {
            this.setStatus('Saving...', 'info');
            
            const response = await fetch('/api/save', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    filename: this.activeTab,
                    content: this.editor.value
                })
            });
            
            const result = await response.json();
            
            if (result.success) {
                const tabData = this.openTabs.get(this.activeTab);
                if (tabData) {
                    tabData.originalContent = this.editor.value;
                    tabData.modified = false;
                    
                    const tab = document.querySelector(`[data-filename="${this.activeTab}"]`);
                    if (tab) {
                        tab.classList.remove('modified');
                    }
                }
                
                this.setStatus(`Saved ${this.activeTab}`, 'success');
            } else {
                this.setStatus(`Error: ${result.error}`, 'error');
            }
        } catch (error) {
            this.setStatus(`Error saving: ${error.message}`, 'error');
        }
    }
    
    createNewFile() {
        const filename = prompt('Enter filename:');
        if (filename) {
            this.createOrSwitchTab(filename, '');
        }
    }
    
    showOpenDialog() {
        this.showFileDialog();
    }
    
    async showFileDialog() {
        const fileDialog = document.getElementById('fileDialog');
        const fileDialogList = document.getElementById('fileDialogList');
        const currentPath = document.getElementById('currentPath');
        const selectedFileName = document.getElementById('selectedFileName');
        
        // Reset dialog state
        selectedFileName.value = '';
        currentPath.textContent = '.';
        this.currentDialogPath = '.';
        
        // Show dialog
        fileDialog.classList.remove('hidden');
        
        // Load files for current directory
        await this.loadDialogFiles('.');
        
        // Focus on filename input
        selectedFileName.focus();
    }
    
    async loadDialogFiles(dir = '.') {
        try {
            const response = await fetch(`/api/list?dir=${encodeURIComponent(dir)}`);
            const result = await response.json();
            
            if (result.success) {
                this.populateDialogFiles(result.files, dir);
                this.currentDialogPath = dir;
                document.getElementById('currentPath').textContent = dir;
            }
        } catch (error) {
            console.error('Error loading dialog files:', error);
        }
    }
    
    populateDialogFiles(files, currentDir) {
        const fileDialogList = document.getElementById('fileDialogList');
        fileDialogList.innerHTML = '';
        
        files.forEach(file => {
            const fileItem = document.createElement('div');
            fileItem.className = 'dialog-file-item';
            
            const icon = document.createElement('span');
            icon.className = 'file-icon';
            
            if (file.endsWith('/')) {
                // Directory
                fileItem.classList.add('directory');
                icon.classList.add('folder');
                fileItem.textContent = file.slice(0, -1);
                
                // Handle directory navigation
                fileItem.addEventListener('click', () => {
                    this.selectDialogItem(fileItem);
                });
                
                fileItem.addEventListener('dblclick', () => {
                    const newDir = currentDir === '.' ? file.slice(0, -1) : currentDir + '/' + file.slice(0, -1);
                    this.loadDialogFiles(newDir);
                });
            } else {
                // File
                const ext = file.split('.').pop()?.toLowerCase() || 'txt';
                icon.classList.add(ext);
                fileItem.textContent = file;
                
                // Handle file selection
                fileItem.addEventListener('click', () => {
                    this.selectDialogItem(fileItem);
                    document.getElementById('selectedFileName').value = file;
                });
                
                fileItem.addEventListener('dblclick', () => {
                    this.selectDialogItem(fileItem);
                    document.getElementById('selectedFileName').value = file;
                    openSelectedFile();
                });
            }
            
            fileItem.insertBefore(icon, fileItem.firstChild);
            fileDialogList.appendChild(fileItem);
        });
    }
    
    selectDialogItem(item) {
        // Clear previous selection
        document.querySelectorAll('.dialog-file-item').forEach(el => {
            el.classList.remove('selected');
        });
        
        // Select current item
        item.classList.add('selected');
    }
    
    showContextMenu(e) {
        e.preventDefault();
        const contextMenu = document.getElementById('contextMenu');
        contextMenu.style.left = `${e.pageX}px`;
        contextMenu.style.top = `${e.pageY}px`;
        contextMenu.classList.remove('hidden');
    }
    
    hideContextMenu() {
        document.getElementById('contextMenu').classList.add('hidden');
    }
    
    updateUI() {
        if (this.activeTab && this.openTabs.has(this.activeTab)) {
            this.noEditorMessage.classList.add('hidden');
            this.editorContainer.classList.remove('hidden');
            this.updateSyntaxHighlighting();
        } else {
            this.noEditorMessage.classList.remove('hidden');
            this.editorContainer.classList.add('hidden');
        }
    }
    
    syncScroll() {
        if (this.syntaxHighlight && this.editor) {
            this.syntaxHighlight.scrollTop = this.editor.scrollTop;
            this.syntaxHighlight.scrollLeft = this.editor.scrollLeft;
        }
    }
    
    updateSyntaxHighlighting() {
        if (!this.activeTab) {
            return;
        }
        
        // Check if this is a Go file
        const isGoFile = this.activeTab.toLowerCase().endsWith('.go');
        
        if (isGoFile) {
            console.log('🐹 Go file detected, enabling syntax highlighting');
            
            // Enable syntax highlighting for Go files
            this.editorContainer.classList.add('syntax-active');
            this.highlightedCode.textContent = this.editor.value;
            
            // Debug: Check if Prism is available
            if (window.Prism) {
                console.log('✅ Prism.js is loaded');
                console.log('📝 Highlighting code:', this.editor.value.substring(0, 50) + '...');
                
                // Use Prism.js to highlight the code
                Prism.highlightElement(this.highlightedCode);
                console.log('🎨 Syntax highlighting applied');
            } else {
                console.error('❌ Prism.js is not loaded');
            }
        } else {
            // Disable syntax highlighting for non-Go files
            console.log('📄 Non-Go file, disabling syntax highlighting');
            this.editorContainer.classList.remove('syntax-active');
        }
    }
    
    updateStatusBar() {
        if (!this.activeTab) return;
        
        // Cursor position
        const cursorPos = this.editor.selectionStart;
        const textBeforeCursor = this.editor.value.substring(0, cursorPos);
        const lines = textBeforeCursor.split('\n');
        const line = lines.length;
        const col = lines[lines.length - 1].length + 1;
        
        this.statusBar.selection.textContent = `Ln ${line}, Col ${col}`;
        
        // File type
        if (this.activeTab) {
            const ext = this.activeTab.split('.').pop()?.toLowerCase();
            const fileTypes = {
                'go': 'Go',
                'js': 'JavaScript',
                'ts': 'TypeScript', 
                'json': 'JSON',
                'md': 'Markdown',
                'css': 'CSS',
                'html': 'HTML',
                'txt': 'Plain Text'
            };
            this.statusBar.fileType.textContent = fileTypes[ext] || 'Plain Text';
        }
        
        // Selection info if text is selected
        if (this.editor.selectionStart !== this.editor.selectionEnd) {
            const selectedText = this.editor.value.substring(
                this.editor.selectionStart, 
                this.editor.selectionEnd
            );
            const selectedLines = selectedText.split('\n').length;
            const selectedChars = selectedText.length;
            
            if (selectedLines > 1) {
                this.statusBar.selection.textContent += ` (${selectedLines} lines, ${selectedChars} chars selected)`;
            } else {
                this.statusBar.selection.textContent += ` (${selectedChars} chars selected)`;
            }
        }
    }
    
    setStatus(message, type) {
        // Could show status in status bar or as notification
        console.log(`[${type.toUpperCase()}] ${message}`);
        
        // You could enhance this to show actual status notifications
        setTimeout(() => {
            // Clear status after some time
        }, 3000);
    }
    
    onBeforeUnload(e) {
        const hasUnsaved = Array.from(this.openTabs.values()).some(tab => tab.modified);
        if (hasUnsaved) {
            e.preventDefault();
            e.returnValue = '';
        }
    }
}

// Context menu functions
function cutText() {
    document.execCommand('cut');
}

function copyText() {
    document.execCommand('copy');
}

function pasteText() {
    document.execCommand('paste');
}

function selectAllText() {
    const editor = document.getElementById('editor');
    editor.select();
}

// File operations
function createNewFile() {
    window.codeEditor?.createNewFile();
}

function refreshExplorer() {
    window.codeEditor?.loadFileTree();
}

function openFileDialog() {
    window.codeEditor?.showOpenDialog();
}

// Menu function implementations
function saveCurrentFile() {
    window.codeEditor?.saveCurrentFile();
}

function saveAsCurrentFile() {
    const filename = prompt('Save as filename:');
    if (filename && window.codeEditor?.activeTab) {
        const editor = window.codeEditor;
        const content = editor.editor.value;
        
        // Create new tab with content
        editor.createOrSwitchTab(filename, content);
        editor.saveCurrentFile();
    }
}

function closeCurrentTab() {
    if (window.codeEditor?.activeTab) {
        window.codeEditor.closeTab(window.codeEditor.activeTab);
    }
}

function closeAllTabs() {
    if (window.codeEditor?.openTabs.size > 0) {
        const hasUnsaved = Array.from(window.codeEditor.openTabs.values()).some(tab => tab.modified);
        if (hasUnsaved && !confirm('Some files have unsaved changes. Close all anyway?')) {
            return;
        }
        
        // Close all tabs
        const tabs = Array.from(window.codeEditor.openTabs.keys());
        tabs.forEach(filename => {
            window.codeEditor.closeTab(filename);
        });
    }
}

function undoAction() {
    document.execCommand('undo');
}

function redoAction() {
    document.execCommand('redo');
}

function findInFile() {
    window.codeEditor?.switchSidePanel('search');
    document.getElementById('searchInput')?.focus();
}

function toggleExplorer() {
    window.codeEditor?.switchSidePanel('explorer');
}

function toggleSearch() {
    window.codeEditor?.switchSidePanel('search');
}

function toggleGitPanel() {
    window.codeEditor?.switchSidePanel('git');
}

function toggleTerminal() {
    // Terminal functionality placeholder
    console.log('Terminal toggle - feature not yet implemented');
    alert('Terminal feature coming soon!');
}

function toggleWordWrap() {
    const editor = document.getElementById('editor');
    if (editor.style.whiteSpace === 'pre-wrap') {
        editor.style.whiteSpace = 'pre';
        console.log('Word wrap disabled');
    } else {
        editor.style.whiteSpace = 'pre-wrap';
        console.log('Word wrap enabled');
    }
}

function zoomIn() {
    const editor = document.getElementById('editor');
    const currentSize = parseInt(getComputedStyle(editor).fontSize);
    editor.style.fontSize = (currentSize + 1) + 'px';
}

function zoomOut() {
    const editor = document.getElementById('editor');
    const currentSize = parseInt(getComputedStyle(editor).fontSize);
    if (currentSize > 10) {
        editor.style.fontSize = (currentSize - 1) + 'px';
    }
}

function showKeyboardShortcuts() {
    alert(`Keyboard Shortcuts:
    
File Operations:
• Ctrl+N - New File
• Ctrl+O - Open File  
• Ctrl+S - Save File
• Ctrl+W - Close Tab

Editor:
• Ctrl+Z - Undo
• Ctrl+Y - Redo
• Ctrl+X - Cut
• Ctrl+C - Copy
• Ctrl+V - Paste
• Ctrl+A - Select All
• Ctrl+F - Find in Files

View:
• Ctrl+Shift+E - Toggle Explorer
• Ctrl+Shift+F - Toggle Search
• Ctrl+Shift+G - Toggle Git Panel
• Ctrl++ - Zoom In
• Ctrl+- - Zoom Out

Editor Features:
• Tab - Indent
• Shift+Tab - Unindent`);
}

function showAbout() {
    alert(`Code Editor v1.0

A VS Code-like web-based text editor built with:
• Go backend server
• HTML5, CSS3, JavaScript frontend
• File system integration
• Multi-tab editing
• Dark theme interface

Features:
✓ Multiple file tabs
✓ File explorer
✓ Syntax highlighting hints
✓ Keyboard shortcuts
✓ Auto-save warnings
✓ Context menus
✓ Status bar information`);
}

function openDocumentation() {
    window.open('https://github.com/anthropics/claude-code', '_blank');
}

function reportIssue() {
    window.open('https://github.com/anthropics/claude-code/issues', '_blank');
}

// File dialog functions
function openSelectedFile() {
    const selectedFileName = document.getElementById('selectedFileName');
    const filename = selectedFileName.value.trim();
    
    if (filename && window.codeEditor) {
        const fullPath = window.codeEditor.currentDialogPath === '.' 
            ? filename 
            : window.codeEditor.currentDialogPath + '/' + filename;
        
        closeFileDialog();
        window.codeEditor.openFile(fullPath);
    }
}

function closeFileDialog() {
    const fileDialog = document.getElementById('fileDialog');
    fileDialog.classList.add('hidden');
    
    // Clear selection and reset state
    document.querySelectorAll('.dialog-file-item').forEach(el => {
        el.classList.remove('selected');
    });
    
    document.getElementById('selectedFileName').value = '';
}

// Initialize the IDE when page loads
document.addEventListener('DOMContentLoaded', () => {
    // Check if Prism.js is loaded
    console.log('🔍 Checking Prism.js availability...');
    if (window.Prism) {
        console.log('✅ Prism.js is available:', window.Prism);
        console.log('🐹 Go language support:', window.Prism.languages?.go ? 'Available' : 'Not found');
    } else {
        console.error('❌ Prism.js is not loaded - syntax highlighting will not work');
    }
    
    window.codeEditor = new CodeEditor();
    
    // Add keyboard support for file dialog
    document.addEventListener('keydown', (e) => {
        const fileDialog = document.getElementById('fileDialog');
        if (!fileDialog.classList.contains('hidden')) {
            if (e.key === 'Escape') {
                e.preventDefault();
                closeFileDialog();
            } else if (e.key === 'Enter') {
                e.preventDefault();
                openSelectedFile();
            }
        }
    });
    
    console.log('🚀 Code Editor initialized');
    console.log('💡 Keyboard shortcuts:');
    console.log('   Ctrl+S: Save file');
    console.log('   Ctrl+N: New file');
    console.log('   Ctrl+O: Open file');
    console.log('   Ctrl+W: Close tab');
    console.log('   Ctrl+F: Focus search');
});