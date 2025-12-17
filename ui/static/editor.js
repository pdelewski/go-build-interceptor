// VS Code-like IDE JavaScript
class CodeEditor {
    constructor() {
        this.openTabs = new Map(); // Map of filename -> tab content
        this.activeTab = null;
        this.currentContent = '';
        this.isModified = false;
        this.activeSidePanel = 'explorer';
        this.selectedExplorerItem = null; // Track selected item in explorer
        
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
        this.initializeResize();
        this.initializeTerminalResize();
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
        
        // File tree keyboard navigation
        this.fileTree.addEventListener('keydown', (e) => this.handleExplorerKeyDown(e));
        
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
            fileItem.dataset.itemType = 'file';
            fileItem.dataset.itemPath = file;
            fileItem.tabIndex = 0; // Make focusable for keyboard navigation
            
            const icon = document.createElement('span');
            icon.className = 'file-icon';
            
            if (file.endsWith('/')) {
                fileItem.classList.add('directory');
                fileItem.dataset.itemType = 'directory';
                icon.classList.add('folder');
                fileItem.textContent = file.slice(0, -1);
            } else {
                const ext = file.split('.').pop()?.toLowerCase() || 'txt';
                icon.classList.add(ext);
                fileItem.textContent = file;
                
                // Add selection and open functionality
                fileItem.addEventListener('click', (e) => {
                    e.preventDefault();
                    this.selectExplorerItem(fileItem);
                });
                fileItem.addEventListener('dblclick', (e) => {
                    e.preventDefault();
                    this.openFile(file);
                });
            }
            
            // Add selection functionality for directories too
            if (file.endsWith('/')) {
                fileItem.addEventListener('click', (e) => {
                    e.preventDefault();
                    this.selectExplorerItem(fileItem);
                });
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
    
    selectExplorerItem(item) {
        // Clear previous selection in explorer
        this.fileTree.querySelectorAll('.file-item, .function-item, .package-item, .pack-info-line').forEach(el => {
            el.classList.remove('selected');
        });
        
        // Select current item
        item.classList.add('selected');
        this.selectedExplorerItem = item;
        
        // Focus the item for keyboard navigation
        item.focus();
        
        // Log selection info
        const itemType = item.dataset.itemType || item.className.split(' ')[0];
        const itemPath = item.dataset.itemPath || item.textContent;
        console.log(`Selected ${itemType}: ${itemPath}`);
    }

    handleExplorerKeyDown(e) {
        // Only handle keys when explorer panel is active
        if (this.activeSidePanel !== 'explorer') return;
        
        const explorerItems = Array.from(this.fileTree.querySelectorAll('.file-item, .function-item, .package-item, .pack-info-line'));
        if (explorerItems.length === 0) return;
        
        let currentIndex = -1;
        if (this.selectedExplorerItem) {
            currentIndex = explorerItems.indexOf(this.selectedExplorerItem);
        }
        
        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                if (currentIndex < explorerItems.length - 1) {
                    this.selectExplorerItem(explorerItems[currentIndex + 1]);
                } else if (currentIndex === -1 && explorerItems.length > 0) {
                    // Select first item if none selected
                    this.selectExplorerItem(explorerItems[0]);
                }
                break;
                
            case 'ArrowUp':
                e.preventDefault();
                if (currentIndex > 0) {
                    this.selectExplorerItem(explorerItems[currentIndex - 1]);
                } else if (currentIndex === -1 && explorerItems.length > 0) {
                    // Select last item if none selected
                    this.selectExplorerItem(explorerItems[explorerItems.length - 1]);
                }
                break;
                
            case 'Enter':
            case ' ': // Space key
                e.preventDefault();
                if (this.selectedExplorerItem) {
                    // Handle different item types
                    if (this.selectedExplorerItem.classList.contains('file-item')) {
                        const itemPath = this.selectedExplorerItem.dataset.itemPath;
                        const itemType = this.selectedExplorerItem.dataset.itemType;
                        
                        if (itemType === 'file' && itemPath && !itemPath.endsWith('/')) {
                            // Open file
                            this.openFile(itemPath);
                        }
                    }
                    // For function, package, and pack-info items, just keep them selected
                    // Could be extended later for more specific actions
                }
                break;
                
            case 'Home':
                e.preventDefault();
                if (explorerItems.length > 0) {
                    this.selectExplorerItem(explorerItems[0]);
                }
                break;
                
            case 'End':
                e.preventDefault();
                if (explorerItems.length > 0) {
                    this.selectExplorerItem(explorerItems[explorerItems.length - 1]);
                }
                break;
        }
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
    
    initializeResize() {
        const resizeHandle = document.getElementById('resizeHandle');
        const sidePanel = document.getElementById('sidePanel');
        
        if (!resizeHandle || !sidePanel) return;
        
        let isResizing = false;
        let startX = 0;
        let startWidth = 0;
        
        resizeHandle.addEventListener('mousedown', (e) => {
            isResizing = true;
            startX = e.clientX;
            startWidth = parseInt(document.defaultView.getComputedStyle(sidePanel).width, 10);
            
            document.body.classList.add('resizing');
            document.addEventListener('mousemove', onMouseMove);
            document.addEventListener('mouseup', onMouseUp);
            
            e.preventDefault();
        });
        
        const onMouseMove = (e) => {
            if (!isResizing) return;

            const currentX = e.clientX;
            const diffX = currentX - startX;
            const newWidth = startWidth + diffX;

            // Constrain to min/max width
            const minWidth = 200;
            const maxWidth = 600;
            const constrainedWidth = Math.max(minWidth, Math.min(maxWidth, newWidth));

            sidePanel.style.width = constrainedWidth + 'px';

            // Update terminal position if it's open
            if (typeof updateTerminalPosition === 'function') {
                updateTerminalPosition();
            }

            e.preventDefault();
        };
        
        const onMouseUp = () => {
            isResizing = false;
            document.body.classList.remove('resizing');
            document.removeEventListener('mousemove', onMouseMove);
            document.removeEventListener('mouseup', onMouseUp);
        };
        
        // Double-click to reset to default width
        resizeHandle.addEventListener('dblclick', () => {
            sidePanel.style.width = '300px';
        });
    }
    
    initializeTerminalResize() {
        const terminalResizeHandle = document.getElementById('terminalResizeHandle');
        const terminalPanel = document.getElementById('terminalPanel');
        
        if (!terminalResizeHandle || !terminalPanel) return;
        
        let isResizing = false;
        let startY = 0;
        let startHeight = 0;
        
        terminalResizeHandle.addEventListener('mousedown', (e) => {
            isResizing = true;
            startY = e.clientY;
            startHeight = parseInt(document.defaultView.getComputedStyle(terminalPanel).height, 10);

            document.body.classList.add('terminal-resizing');
            document.addEventListener('mousemove', onMouseMove);
            document.addEventListener('mouseup', onMouseUp);

            e.preventDefault();
        });
        
        const onMouseMove = (e) => {
            if (!isResizing) return;
            
            const currentY = e.clientY;
            const diffY = startY - currentY; // Reversed because we're resizing from top
            const newHeight = startHeight + diffY;
            
            // Constrain to min/max height
            const minHeight = 150;
            const maxHeight = Math.min(window.innerHeight * 0.8, 600);
            const constrainedHeight = Math.max(minHeight, Math.min(maxHeight, newHeight));
            
            terminalPanel.style.height = constrainedHeight + 'px';
            
            // Update the layout adjustment
            if (document.body.classList.contains('terminal-open')) {
                const ideContainer = document.querySelector('.ide-container');
                if (ideContainer) {
                    ideContainer.style.bottom = (constrainedHeight + 22) + 'px'; // height + status bar
                }
            }
            
            e.preventDefault();
        };
        
        const onMouseUp = () => {
            isResizing = false;
            document.body.classList.remove('terminal-resizing');
            document.removeEventListener('mousemove', onMouseMove);
            document.removeEventListener('mouseup', onMouseUp);
        };

        // Double-click to reset to default height
        terminalResizeHandle.addEventListener('dblclick', () => {
            terminalPanel.style.height = '300px';
            
            // Update the layout adjustment
            if (document.body.classList.contains('terminal-open')) {
                const ideContainer = document.querySelector('.ide-container');
                if (ideContainer) {
                    ideContainer.style.bottom = '322px'; // 300px + 22px status bar
                }
            }
        });
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

async function showFunctions() {
    // Call external go-build-interceptor --pack-functions and show output in explorer
    console.log('Functions view - calling go-build-interceptor --pack-functions');
    
    try {
        // Switch to explorer panel first
        window.codeEditor?.switchSidePanel('explorer');
        
        // Show loading message
        const fileTree = document.getElementById('fileTree');
        if (fileTree) {
            fileTree.innerHTML = '<div class="loading-message">⚙️ Running go-build-interceptor --pack-functions...</div>';
        }
        
        // Call the API endpoint
        const response = await fetch('/api/pack-functions');
        const result = await response.json();
        
        if (result.success) {
            // Display the command output in the file tree
            const output = result.content;
            console.log('Pack functions output:', output);
            
            if (fileTree) {
                // Parse and format the output as function signatures
                const lines = output.split('\n').filter(line => line.trim() !== '');
                fileTree.innerHTML = '';
                
                // Add a header
                const header = document.createElement('div');
                header.className = 'view-header';
                header.innerHTML = `
                    ⚙️ FUNCTIONS
                    <button onclick="loadFilesIntoExplorer()" style="margin-left: 10px; padding: 3px 8px; background: #007acc; color: white; border: none; border-radius: 3px; cursor: pointer; font-size: 11px;">
                        ← Back to Files
                    </button>
                `;
                fileTree.appendChild(header);
                
                // Group functions by file or show all
                lines.forEach(line => {
                    let trimmedLine = line.trim();
                    
                    // Remove leading '-' if present
                    if (trimmedLine.startsWith('- ')) {
                        trimmedLine = trimmedLine.substring(2);
                    } else if (trimmedLine.startsWith('-')) {
                        trimmedLine = trimmedLine.substring(1);
                    }
                    
                    if (trimmedLine) {
                        // Try to parse function information
                        const functionItem = document.createElement('div');
                        functionItem.className = 'explorer-item function-item';
                        functionItem.tabIndex = 0; // Make focusable for keyboard navigation
                        
                        // Add checkbox for selection
                        const checkbox = document.createElement('input');
                        checkbox.type = 'checkbox';
                        checkbox.className = 'explorer-checkbox';
                        checkbox.addEventListener('click', (e) => {
                            e.stopPropagation(); // Prevent item selection when clicking checkbox
                        });
                        
                        // Add item content wrapper
                        const itemContent = document.createElement('div');
                        itemContent.className = 'explorer-item-content';
                        
                        // Add selection event listeners
                        functionItem.addEventListener('click', (e) => {
                            e.preventDefault();
                            window.codeEditor?.selectExplorerItem(functionItem);
                        });
                        
                        // Set text content and title
                        itemContent.textContent = trimmedLine;
                        functionItem.title = trimmedLine;
                        
                        // Append checkbox and content to the function item
                        functionItem.appendChild(checkbox);
                        functionItem.appendChild(itemContent);
                        
                        fileTree.appendChild(functionItem);
                    }
                });
                
                // If no functions found, show message
                if (lines.length === 0) {
                    const noFunctions = document.createElement('div');
                    noFunctions.className = 'no-functions-message';
                    noFunctions.style.cssText = 'padding: 8px; color: #999; font-style: italic;';
                    noFunctions.textContent = 'No functions found.';
                    fileTree.appendChild(noFunctions);
                }
            }
        } else {
            // Show error message
            if (fileTree) {
                fileTree.innerHTML = `<div class="error-message" style="padding: 8px; color: #ff6b6b;">❌ Error: ${result.error}</div>`;
            }
            console.error('Pack functions error:', result.error);
        }
    } catch (error) {
        // Show network error
        const fileTree = document.getElementById('fileTree');
        if (fileTree) {
            fileTree.innerHTML = `<div class="error-message" style="padding: 8px; color: #ff6b6b;">❌ Network Error: ${error.message}</div>`;
        }
        console.error('Network error calling pack-functions:', error);
    }
}

async function showFiles() {
    // Call external go-build-interceptor --pack-files and show output in explorer
    console.log('Files view - calling go-build-interceptor --pack-files');
    
    try {
        // Switch to explorer panel first
        window.codeEditor?.switchSidePanel('explorer');
        
        // Show loading message
        const fileTree = document.getElementById('fileTree');
        if (fileTree) {
            fileTree.innerHTML = '<div class="loading-message">🔍 Running go-build-interceptor --pack-files...</div>';
        }
        
        // Call the API endpoint
        const response = await fetch('/api/pack-files');
        const result = await response.json();
        
        if (result.success) {
            // Display the command output in the file tree
            const output = result.content;
            console.log('Pack files output:', output);
            
            if (fileTree) {
                // Parse and format the output as clickable Go files
                const lines = output.split('\n').filter(line => line.trim() !== '');
                fileTree.innerHTML = '';
                
                // Add a header
                const header = document.createElement('div');
                header.className = 'view-header';
                header.innerHTML = `
                    📦 PACK FILES
                    <button onclick="loadFilesIntoExplorer()" style="margin-left: 10px; padding: 3px 8px; background: #007acc; color: white; border: none; border-radius: 3px; cursor: pointer; font-size: 11px;">
                        ← Back to Files
                    </button>
                `;
                fileTree.appendChild(header);
                
                // Add each Go file as a clickable file item
                lines.forEach(line => {
                    const trimmedLine = line.trim();
                    if (trimmedLine && trimmedLine.endsWith('.go')) {
                        const fileItem = document.createElement('div');
                        fileItem.className = 'explorer-item file-item';
                        fileItem.tabIndex = 0; // Make focusable for keyboard navigation
                        
                        // Add checkbox for selection
                        const checkbox = document.createElement('input');
                        checkbox.type = 'checkbox';
                        checkbox.className = 'explorer-checkbox';
                        checkbox.addEventListener('click', (e) => {
                            e.stopPropagation();
                        });
                        
                        // Create content wrapper
                        const contentWrapper = document.createElement('div');
                        contentWrapper.className = 'explorer-item-content';
                        
                        // Add icon and filename
                        const icon = document.createElement('span');
                        icon.className = 'file-icon go';
                        
                        // Extract just the filename for display
                        const filename = trimmedLine.split('/').pop();
                        contentWrapper.appendChild(icon);
                        contentWrapper.appendChild(document.createTextNode(filename));
                        
                        // Add selection event listeners
                        fileItem.addEventListener('click', (e) => {
                            e.preventDefault();
                            window.codeEditor?.selectExplorerItem(fileItem);
                        });
                        
                        // Make it double-clickable to open the file
                        fileItem.addEventListener('dblclick', () => {
                            console.log('Double-clicking pack file:', trimmedLine);
                            window.codeEditor?.openFile(trimmedLine);
                        });
                        
                        fileItem.appendChild(checkbox);
                        fileItem.appendChild(contentWrapper);
                        fileItem.title = trimmedLine; // Show full path on hover
                        
                        fileTree.appendChild(fileItem);
                    }
                    // Remove the else if block - don't show non-Go files
                });
                
                // If no files found, show message
                const goFiles = lines.filter(line => line.trim().endsWith('.go'));
                if (goFiles.length === 0) {
                    const noFiles = document.createElement('div');
                    noFiles.className = 'no-files-message';
                    noFiles.style.cssText = 'padding: 8px; color: #999; font-style: italic;';
                    noFiles.textContent = 'No Go files found in pack commands.';
                    fileTree.appendChild(noFiles);
                }
            }
        } else {
            // Show error message
            if (fileTree) {
                fileTree.innerHTML = `<div class="error-message" style="padding: 8px; color: #ff6b6b;">❌ Error: ${result.error}</div>`;
            }
            console.error('Pack files error:', result.error);
        }
    } catch (error) {
        // Show network error
        const fileTree = document.getElementById('fileTree');
        if (fileTree) {
            fileTree.innerHTML = `<div class="error-message" style="padding: 8px; color: #ff6b6b;">❌ Network Error: ${error.message}</div>`;
        }
        console.error('Network error calling pack-files:', error);
    }
}

function showProject() {
    // Switch to explorer panel to show project files from --dir
    console.log('Project view - switching to explorer panel');
    window.codeEditor?.switchSidePanel('explorer');
    // Refresh the file tree to ensure it shows current project state
    window.codeEditor?.loadFileTree();
}

// Status update function
function updateStatus(message) {
    console.log('[STATUS] ' + message);
    // Could enhance this to show in status bar
}

async function showStaticCallGraph() {
    try {
        console.log('📊 Fetching static call graph...');
        
        const response = await fetch('/api/callgraph');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const responseData = await response.json();
        const callGraphData = responseData.content;
        
        // Parse the call graph into a tree structure
        console.log('Raw call graph data:', callGraphData.substring(0, 500));
        const callTree = parseCallGraph(callGraphData);
        console.log('Parsed call tree:', callTree);
        
        // Clear existing content and show call graph
        const fileTree = document.getElementById('fileTree');
        fileTree.innerHTML = '';
        
        // Add header
        const header = document.createElement('div');
        header.className = 'view-header';
        header.innerHTML = `
            📊 Static Call Graph
            <button onclick="loadFilesIntoExplorer()" style="margin-left: 10px; padding: 3px 8px; background: #007acc; color: white; border: none; border-radius: 3px; cursor: pointer; font-size: 11px;">
                ← Back to Files
            </button>
        `;
        fileTree.appendChild(header);
        
        // If parsing failed, show raw data as fallback
        if (callTree.length === 0) {
            const fallbackDiv = document.createElement('div');
            fallbackDiv.style.cssText = 'padding: 10px; font-family: monospace; font-size: 12px; color: #ddd; white-space: pre-wrap; max-height: 400px; overflow: auto; background: #2d2d2d; margin: 10px 0; border-radius: 4px;';
            fallbackDiv.textContent = callGraphData;
            fileTree.appendChild(fallbackDiv);
            
            const debugDiv = document.createElement('div');
            debugDiv.style.cssText = 'padding: 10px; color: #ff9999; font-size: 11px;';
            debugDiv.textContent = 'Debug: Parser returned empty tree. Showing raw data above.';
            fileTree.appendChild(debugDiv);
        } else {
            // Render the call tree
            renderCallTree(fileTree, callTree);
        }
        
        console.log('✅ Call graph displayed successfully');
        
    } catch (error) {
        console.error('❌ Error fetching call graph:', error);
        alert(`Failed to generate call graph: ${error.message}`);
    }
}

function parseCallGraph(callGraphText) {
    const lines = callGraphText.split('\n');
    const tree = [];
    let nodeStack = []; // Stack to track nested calls
    
    for (let line of lines) {
        // Skip empty lines and headers
        if (!line.trim() || line.includes('===') || line.includes('Summary:') || line.includes('Parsed ') || line.includes('Call Graph Mode') || line.includes('Processed ')) {
            continue;
        }
        
        // Check for root function (ends with colon, no indentation)
        if (line.match(/^[a-zA-Z_][a-zA-Z0-9_]*:\s*$/)) {
            const funcName = line.replace(':', '').trim();
            const rootNode = {
                name: funcName,
                children: [],
                isRoot: true,
                expanded: true // Start expanded by default
            };
            tree.push(rootNode);
            nodeStack = [rootNode]; // Reset stack with new root
            continue;
        }
        
        // Parse function calls with arrows - handle both -> and encoded \u003e arrows
        // Also handle Unicode arrows and HTML entities
        const normalizedLine = line.replace(/\\u003e/g, '>').replace(/&gt;/g, '>').replace(/→/g, '>');
        const arrowMatch = normalizedLine.match(/^(\s+)->\s*(.+?)\s*\(line[s]?\s*([\d,\s]+)\)$/);
        if (arrowMatch && nodeStack.length > 0) {
            const [, spaces, funcName, lineNumbers] = arrowMatch;
            
            // Calculate indentation level (each 2 spaces = 1 level, starting from 1)
            const indentLevel = Math.floor(spaces.length / 2);
            
            const node = {
                name: funcName.trim(),
                lines: lineNumbers.trim(),
                children: [],
                isRoot: false,
                expanded: true
            };
            
            // Adjust stack to correct level
            while (nodeStack.length > indentLevel) {
                nodeStack.pop();
            }
            
            // Add to the appropriate parent (top of stack)
            if (nodeStack.length > 0) {
                nodeStack[nodeStack.length - 1].children.push(node);
                nodeStack.push(node); // Push this node to stack for potential children
            }
        }
    }
    
    return tree;
}

function renderCallTree(container, nodes, level = 0) {
    nodes.forEach(node => {
        const nodeItem = document.createElement('div');
        nodeItem.className = 'call-graph-item';
        nodeItem.style.paddingLeft = (level * 2) + 'px';
        
        const nodeContent = document.createElement('div');
        nodeContent.className = 'call-graph-content';
        nodeContent.style.display = 'flex';
        nodeContent.style.alignItems = 'center';
        nodeContent.style.padding = '2px 0';
        nodeContent.style.cursor = 'pointer';
        
        // Add expand/collapse arrow if has children
        const arrow = document.createElement('span');
        arrow.className = 'call-graph-arrow';
        arrow.style.marginRight = '5px';
        arrow.style.width = '12px';
        arrow.style.display = 'inline-block';
        arrow.style.fontSize = '10px';
        
        if (node.children.length > 0) {
            arrow.textContent = node.expanded ? '▼' : '▶';
            arrow.style.cursor = 'pointer';
        } else {
            arrow.textContent = '';
        }
        
        // Add function name
        const nameSpan = document.createElement('span');
        nameSpan.textContent = node.name;
        nameSpan.style.color = node.isRoot ? '#4fc3f7' : '#e0e0e0';
        nameSpan.style.fontWeight = node.isRoot ? 'bold' : 'normal';
        
        // Add line numbers if present
        if (node.lines && !node.isRoot) {
            const linesSpan = document.createElement('span');
            linesSpan.textContent = ` (lines ${node.lines})`;
            linesSpan.style.color = '#666';
            linesSpan.style.fontSize = '11px';
            linesSpan.style.marginLeft = '5px';
            nodeContent.appendChild(nameSpan);
            nodeContent.appendChild(linesSpan);
        } else {
            nodeContent.appendChild(nameSpan);
        }
        
        nodeContent.insertBefore(arrow, nameSpan);
        
        // Add click handler for expand/collapse
        if (node.children.length > 0) {
            nodeContent.addEventListener('click', (e) => {
                e.preventDefault();
                node.expanded = !node.expanded;
                arrow.textContent = node.expanded ? '▼' : '▶';
                
                // Show/hide children
                const childrenContainer = nodeItem.querySelector('.call-graph-children');
                if (childrenContainer) {
                    childrenContainer.style.display = node.expanded ? 'block' : 'none';
                }
            });
        }
        
        nodeItem.appendChild(nodeContent);
        
        // Add children container
        if (node.children.length > 0) {
            const childrenContainer = document.createElement('div');
            childrenContainer.className = 'call-graph-children';
            childrenContainer.style.display = node.expanded ? 'block' : 'none';
            
            renderCallTree(childrenContainer, node.children, level + 1);
            nodeItem.appendChild(childrenContainer);
        }
        
        container.appendChild(nodeItem);
    });
}

async function showPackages() {
    // Call external go-build-interceptor --pack-packages and show output in explorer
    console.log('Packages view - calling go-build-interceptor --pack-packages');
    
    try {
        // Switch to explorer panel first
        window.codeEditor?.switchSidePanel('explorer');
        
        // Show loading message
        const fileTree = document.getElementById('fileTree');
        if (fileTree) {
            fileTree.innerHTML = '<div class="loading-message">📦 Running go-build-interceptor --pack-packages...</div>';
        }
        
        // Call the API endpoint
        const response = await fetch('/api/pack-packages');
        const result = await response.json();
        
        if (result.success) {
            // Display the command output in the file tree
            const output = result.content;
            console.log('Pack packages output:', output);
            
            if (fileTree) {
                // Parse and format the output as package information
                const lines = output.split('\n').filter(line => line.trim() !== '');
                fileTree.innerHTML = '';
                
                // Add a header
                const header = document.createElement('div');
                header.className = 'view-header';
                header.innerHTML = `
                    📦 PACKAGES
                    <button onclick="loadFilesIntoExplorer()" style="margin-left: 10px; padding: 3px 8px; background: #007acc; color: white; border: none; border-radius: 3px; cursor: pointer; font-size: 11px;">
                        ← Back to Files
                    </button>
                `;
                fileTree.appendChild(header);
                
                // Display package information
                lines.forEach(line => {
                    let trimmedLine = line.trim();
                    
                    // Remove leading '-' if present
                    if (trimmedLine.startsWith('- ')) {
                        trimmedLine = trimmedLine.substring(2);
                    } else if (trimmedLine.startsWith('-')) {
                        trimmedLine = trimmedLine.substring(1);
                    }
                    
                    if (trimmedLine) {
                        const packageItem = document.createElement('div');
                        packageItem.className = 'explorer-item package-item';
                        packageItem.tabIndex = 0; // Make focusable for keyboard navigation
                        
                        // Add checkbox for selection
                        const checkbox = document.createElement('input');
                        checkbox.type = 'checkbox';
                        checkbox.className = 'explorer-checkbox';
                        checkbox.addEventListener('click', (e) => {
                            e.stopPropagation();
                        });
                        
                        // Create content wrapper
                        const contentWrapper = document.createElement('div');
                        contentWrapper.className = 'explorer-item-content';
                        contentWrapper.textContent = trimmedLine;
                        
                        // Add selection event listeners
                        packageItem.addEventListener('click', (e) => {
                            e.preventDefault();
                            window.codeEditor?.selectExplorerItem(packageItem);
                        });
                        
                        packageItem.appendChild(checkbox);
                        packageItem.appendChild(contentWrapper);
                        packageItem.title = trimmedLine; // Show full name on hover
                        
                        fileTree.appendChild(packageItem);
                    }
                });
                
                // If no packages found, show message
                if (lines.length === 0) {
                    const noPackages = document.createElement('div');
                    noPackages.className = 'explorer-item pack-info-line';
                    noPackages.textContent = 'No packages found.';
                    fileTree.appendChild(noPackages);
                }
            }
        } else {
            // Show error message
            if (fileTree) {
                fileTree.innerHTML = `<div class="error-message" style="padding: 8px; color: #ff6b6b;">❌ Error: ${result.error}</div>`;
            }
            console.error('Pack packages error:', result.error);
        }
    } catch (error) {
        // Show network error
        const fileTree = document.getElementById('fileTree');
        if (fileTree) {
            fileTree.innerHTML = `<div class="error-message" style="padding: 8px; color: #ff6b6b;">❌ Network Error: ${error.message}</div>`;
        }
        console.error('Network error calling pack-packages:', error);
    }
}

async function showWorkDirectory() {
    try {
        console.log('📁 Fetching work directory info...');
        
        const response = await fetch('/api/workdir');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const responseData = await response.json();
        if (!responseData.success) {
            throw new Error(responseData.error || 'Failed to fetch work directory');
        }
        
        const workDirData = responseData.content;
        
        // Clear existing content and show work directory
        const fileTree = document.getElementById('fileTree');
        fileTree.innerHTML = '';
        
        // Add header
        const header = document.createElement('div');
        header.className = 'view-header';
        header.innerHTML = `
            📁 Work Directory
            <button onclick="loadFilesIntoExplorer()" style="margin-left: 10px; padding: 3px 8px; background: #007acc; color: white; border: none; border-radius: 3px; cursor: pointer; font-size: 11px;">
                ← Back to Files
            </button>
        `;
        fileTree.appendChild(header);
        
        // Create content container
        const content = document.createElement('div');
        content.className = 'work-dir-content';
        content.style.cssText = `
            padding: 10px;
            font-family: 'Courier New', monospace;
            font-size: 12px;
            line-height: 1.4;
            white-space: pre-wrap;
            overflow-y: auto;
            max-height: calc(100vh - 150px);
        `;
        
        // Format the work directory output
        content.textContent = workDirData;
        
        fileTree.appendChild(content);
        
    } catch (error) {
        console.error('Error fetching work directory:', error);
        alert(`Failed to generate work directory: ${error.message}`);
    }
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
    const syntaxHighlight = document.getElementById('syntaxHighlight');
    const currentSize = parseInt(getComputedStyle(editor).fontSize);
    const newSize = currentSize + 1;
    
    // Set font size with !important to override CSS
    editor.style.setProperty('font-size', newSize + 'px', 'important');
    
    if (syntaxHighlight) {
        syntaxHighlight.style.setProperty('font-size', newSize + 'px', 'important');
        
        // Also update Prism.js elements
        const preElement = syntaxHighlight.querySelector('pre[class*="language-"]');
        const codeElement = syntaxHighlight.querySelector('code[class*="language-"]');
        
        if (preElement) {
            preElement.style.setProperty('font-size', newSize + 'px', 'important');
        }
        if (codeElement) {
            codeElement.style.setProperty('font-size', newSize + 'px', 'important');
        }
    }
}

function zoomOut() {
    const editor = document.getElementById('editor');
    const syntaxHighlight = document.getElementById('syntaxHighlight');
    const currentSize = parseInt(getComputedStyle(editor).fontSize);
    
    if (currentSize > 10) {
        const newSize = currentSize - 1;
        
        // Set font size with !important to override CSS
        editor.style.setProperty('font-size', newSize + 'px', 'important');
        
        if (syntaxHighlight) {
            syntaxHighlight.style.setProperty('font-size', newSize + 'px', 'important');
            
            // Also update Prism.js elements
            const preElement = syntaxHighlight.querySelector('pre[class*="language-"]');
            const codeElement = syntaxHighlight.querySelector('code[class*="language-"]');
            
            if (preElement) {
                preElement.style.setProperty('font-size', newSize + 'px', 'important');
            }
            if (codeElement) {
                codeElement.style.setProperty('font-size', newSize + 'px', 'important');
            }
        }
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

// Utility function to escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Function to load files back into explorer
function loadFilesIntoExplorer() {
    window.codeEditor?.switchSidePanel('explorer');
    window.codeEditor?.loadFileTree();
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

// Terminal functionality
function toggleTerminal() {
    const terminal = document.getElementById('terminalPanel');
    const isVisible = terminal.style.display === 'flex';
    
    if (isVisible) {
        closeTerminal();
    } else {
        showTerminal();
    }
}

function showTerminal() {
    const terminal = document.getElementById('terminalPanel');
    terminal.style.display = 'flex';
    document.body.classList.add('terminal-open');

    // Update layout based on current terminal height
    const terminalHeight = parseInt(document.defaultView.getComputedStyle(terminal).height, 10);
    const ideContainer = document.querySelector('.ide-container');
    if (ideContainer) {
        ideContainer.style.bottom = (terminalHeight + 22) + 'px'; // height + status bar
    }

    // Auto-scroll to bottom
    const terminalContent = document.getElementById('terminalContent');
    terminalContent.scrollTop = terminalContent.scrollHeight;
}

function updateTerminalPosition() {
    const terminal = document.getElementById('terminalPanel');
    const sidePanel = document.getElementById('sidePanel');
    const activityBar = document.querySelector('.activity-bar');

    if (terminal) {
        // Get actual widths - use fallbacks if elements not found or have 0 width
        let activityBarWidth = activityBar ? activityBar.getBoundingClientRect().width : 0;
        if (activityBarWidth <= 0) activityBarWidth = 48; // Default activity bar width

        let sidePanelWidth = sidePanel ? sidePanel.getBoundingClientRect().width : 0;
        const sidePanelHidden = sidePanel && sidePanel.classList.contains('hidden');

        // If side panel is not hidden but has 0 width, use default
        if (!sidePanelHidden && sidePanelWidth <= 0) {
            sidePanelWidth = 300; // Default side panel width
        }

        let leftPosition;
        if (sidePanelHidden) {
            leftPosition = activityBarWidth;
        } else {
            leftPosition = activityBarWidth + sidePanelWidth;
        }

        terminal.style.left = leftPosition + 'px';
        console.log('Terminal position updated:', leftPosition, 'px (activity:', activityBarWidth, 'side:', sidePanelWidth, 'hidden:', sidePanelHidden, ')');
    }
}

function closeTerminal() {
    const terminal = document.getElementById('terminalPanel');
    terminal.style.display = 'none';
    document.body.classList.remove('terminal-open');
    
    // Reset layout
    const ideContainer = document.querySelector('.ide-container');
    if (ideContainer) {
        ideContainer.style.bottom = '22px'; // Just status bar height
    }
}

function clearTerminal() {
    const terminalContent = document.getElementById('terminalContent');
    terminalContent.innerHTML = '';
}

function addTerminalOutput(text, className = '') {
    const terminalContent = document.getElementById('terminalContent');
    const outputDiv = document.createElement('div');
    outputDiv.className = 'terminal-output' + (className ? ' ' + className : '');
    outputDiv.textContent = text;
    terminalContent.appendChild(outputDiv);
    
    // Auto-scroll to bottom
    terminalContent.scrollTop = terminalContent.scrollHeight;
}

// Update the runCompile function to show output in message window
async function runCompile() {
    const hooksFile = prompt('Enter hooks file path (e.g., ../hello_hook/hello_hooks.go):');

    if (!hooksFile || hooksFile.trim() === '') {
        return;
    }

    // Show loading message
    showMessageWindow('Compiling...', 'Running: go-build-interceptor --compile ' + hooksFile.trim(), 'info');

    try {
        const response = await fetch('/api/compile', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ hooksFile: hooksFile.trim() })
        });

        const data = await response.json();

        if (data.error) {
            showMessageWindow('Compile Failed', data.error, 'error');
        } else {
            // Use data.content (from backend) - show full output
            const output = data.content || 'Compile completed successfully (no output)';
            showMessageWindow('Compile Output', output, 'success');
        }
    } catch (err) {
        console.error('Compile error:', err);
        showMessageWindow('Compile Error', 'Failed to run compile: ' + err.message, 'error');
    }
}

// Message window functions - simple compact window with scrollbar
function showMessageWindow(title, message, type = 'info') {
    // Remove existing message window if present
    const existing = document.getElementById('messageWindow');
    if (existing) {
        existing.remove();
    }

    // Create message window
    const messageWindow = document.createElement('div');
    messageWindow.id = 'messageWindow';
    messageWindow.className = 'message-window';

    // Determine icon and color based on type
    let icon = 'ℹ️';
    let headerClass = 'message-header-info';
    if (type === 'error') {
        icon = '❌';
        headerClass = 'message-header-error';
    } else if (type === 'success') {
        icon = '✅';
        headerClass = 'message-header-success';
    } else if (type === 'warning') {
        icon = '⚠️';
        headerClass = 'message-header-warning';
    }

    messageWindow.innerHTML = `
        <div class="message-window-header ${headerClass}">
            <span class="message-title">${icon} ${title}</span>
            <button class="message-close" onclick="closeMessageWindow()">×</button>
        </div>
        <div class="message-window-content">
            <pre class="message-text">${escapeHtml(message)}</pre>
        </div>
    `;

    document.body.appendChild(messageWindow);
}

function closeMessageWindow() {
    const messageWindow = document.getElementById('messageWindow');
    if (messageWindow) {
        messageWindow.remove();
    }
}

// Run the built executable
async function runExecutable() {
    const execPath = prompt('Enter executable path (e.g., ./hello or hello):');

    if (!execPath || execPath.trim() === '') {
        return;
    }

    // Show loading message
    showMessageWindow('Running...', 'Executing: ' + execPath.trim(), 'info');

    try {
        const response = await fetch('/api/run-executable', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ executablePath: execPath.trim() })
        });

        const data = await response.json();

        if (data.error) {
            showMessageWindow('Execution Failed', data.error, 'error');
        } else {
            const output = data.content || 'No output';
            showMessageWindow('Execution Output', output, 'success');
        }
    } catch (err) {
        console.error('Execution error:', err);
        showMessageWindow('Execution Error', 'Failed to run executable: ' + err.message, 'error');
    }
}