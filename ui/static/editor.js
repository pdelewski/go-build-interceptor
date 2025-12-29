// VS Code-like IDE JavaScript with Monaco Editor and LSP support
class CodeEditor {
    constructor() {
        this.openTabs = new Map(); // Map of filename -> tab content
        this.activeTab = null;
        this.currentContent = '';
        this.isModified = false;
        this.activeSidePanel = 'explorer';
        this.selectedExplorerItem = null; // Track selected item in explorer

        // Monaco editor instance
        this.monacoEditor = null;
        this.monacoModels = new Map(); // Map of filename -> monaco model

        // Breakpoints tracking: Map of filename -> Set of line numbers
        this.breakpoints = new Map();
        this.breakpointDecorations = new Map(); // Map of filename -> decoration IDs

        // LSP WebSocket connection
        this.lspSocket = null;
        this.lspRequestId = 0;
        this.lspPendingRequests = new Map();

        // DOM elements
        this.editorContainer = document.getElementById('editorContainer');
        this.monacoContainer = document.getElementById('monacoEditor');
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

        // Initialize Monaco first, then other components
        this.initializeMonaco();
    }

    initializeMonaco() {
        require(['vs/editor/editor.main'], () => {
            // Create Monaco editor instance
            this.monacoEditor = monaco.editor.create(this.monacoContainer, {
                value: '',
                language: 'go',
                theme: 'vs-dark',
                automaticLayout: true,
                minimap: { enabled: true },
                fontSize: 14,
                fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', 'Courier New', monospace",
                lineNumbers: 'on',
                scrollBeyondLastLine: false,
                wordWrap: 'off',
                tabSize: 4,
                insertSpaces: false,
                renderWhitespace: 'selection',
                cursorBlinking: 'smooth',
                cursorSmoothCaretAnimation: 'on',
                smoothScrolling: true,
                bracketPairColorization: { enabled: true },
                guides: {
                    bracketPairs: true,
                    indentation: true
                },
                glyphMargin: true  // Enable glyph margin for breakpoints
            });

            // Set up editor event listeners
            this.monacoEditor.onDidChangeModelContent(() => this.onEditorChange());
            this.monacoEditor.onDidChangeCursorPosition(() => this.updateStatusBar());
            this.monacoEditor.onDidChangeCursorSelection(() => this.updateStatusBar());

            // Add keyboard shortcuts
            this.monacoEditor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
                this.saveCurrentFile();
            });

            // Add F9 shortcut to toggle breakpoint at current line
            this.monacoEditor.addCommand(monaco.KeyCode.F9, () => {
                const position = this.monacoEditor.getPosition();
                if (position && this.activeTab) {
                    this.toggleBreakpoint(this.activeTab, position.lineNumber);
                }
            });

            // Add click handler for glyph margin (breakpoints)
            this.monacoEditor.onMouseDown((e) => {
                if (e.target.type === monaco.editor.MouseTargetType.GUTTER_GLYPH_MARGIN) {
                    const lineNumber = e.target.position?.lineNumber;
                    if (lineNumber && this.activeTab) {
                        this.toggleBreakpoint(this.activeTab, lineNumber);
                    }
                }
            });

            // Initialize LSP connection
            this.initializeLSP();

            // Register Monaco providers for Go
            this.registerGoProviders();

            // Now initialize other components
            this.initializeEventListeners();
            this.loadFileTree();
            this.updateUI();
            this.initializeResize();
            this.initializeTerminalResize();

            console.log('Monaco Editor initialized successfully');
        });
    }

    initializeLSP() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/lsp`;

        try {
            this.lspSocket = new WebSocket(wsUrl);

            this.lspSocket.onopen = () => {
                console.log('LSP WebSocket connected');
                this.sendLSPInitialize();
            };

            this.lspSocket.onmessage = (event) => {
                this.handleLSPMessage(JSON.parse(event.data));
            };

            this.lspSocket.onerror = (error) => {
                console.error('LSP WebSocket error:', error);
            };

            this.lspSocket.onclose = () => {
                console.log('LSP WebSocket closed, reconnecting in 5s...');
                setTimeout(() => this.initializeLSP(), 5000);
            };
        } catch (error) {
            console.error('Failed to connect to LSP:', error);
        }
    }

    sendLSPRequest(method, params, timeout = 5000) {
        if (!this.lspSocket || this.lspSocket.readyState !== WebSocket.OPEN) {
            console.warn('LSP socket not ready');
            return Promise.reject(new Error('LSP not connected'));
        }

        const id = ++this.lspRequestId;
        const message = {
            jsonrpc: '2.0',
            id: id,
            method: method,
            params: params
        };

        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => {
                this.lspPendingRequests.delete(id);
                reject(new Error('LSP request timeout'));
            }, timeout);

            this.lspPendingRequests.set(id, {
                resolve: (result) => {
                    clearTimeout(timer);
                    resolve(result);
                },
                reject: (err) => {
                    clearTimeout(timer);
                    reject(err);
                }
            });

            this.lspSocket.send(JSON.stringify(message));
        });
    }

    registerGoProviders() {
        const self = this;

        // Completion provider
        monaco.languages.registerCompletionItemProvider('go', {
            triggerCharacters: ['.', '(', '"', '/'],
            provideCompletionItems: async (model, position) => {
                if (!self.lspSocket || self.lspSocket.readyState !== WebSocket.OPEN) {
                    return { suggestions: [] };
                }

                const filename = self.activeTab;
                if (!filename) return { suggestions: [] };

                try {
                    const result = await self.sendLSPRequest('textDocument/completion', {
                        textDocument: { uri: self.getDocumentUri(filename) },
                        position: {
                            line: position.lineNumber - 1,
                            character: position.column - 1
                        }
                    });

                    if (!result) return { suggestions: [] };

                    const items = result.items || result || [];
                    const suggestions = items.map(item => ({
                        label: item.label,
                        kind: self.lspCompletionKindToMonaco(item.kind),
                        detail: item.detail || '',
                        documentation: item.documentation ?
                            (typeof item.documentation === 'string' ? item.documentation : item.documentation.value) : '',
                        insertText: item.insertText || item.label,
                        insertTextRules: item.insertTextFormat === 2 ?
                            monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet : undefined,
                        range: undefined
                    }));

                    return { suggestions };
                } catch (err) {
                    console.error('Completion error:', err);
                    return { suggestions: [] };
                }
            }
        });

        // Hover provider
        monaco.languages.registerHoverProvider('go', {
            provideHover: async (model, position) => {
                if (!self.lspSocket || self.lspSocket.readyState !== WebSocket.OPEN) {
                    return null;
                }

                const filename = self.activeTab;
                if (!filename) return null;

                try {
                    const result = await self.sendLSPRequest('textDocument/hover', {
                        textDocument: { uri: self.getDocumentUri(filename) },
                        position: {
                            line: position.lineNumber - 1,
                            character: position.column - 1
                        }
                    });

                    if (!result || !result.contents) return null;

                    let contents = [];
                    if (typeof result.contents === 'string') {
                        contents = [{ value: result.contents }];
                    } else if (result.contents.value) {
                        contents = [{ value: result.contents.value }];
                    } else if (Array.isArray(result.contents)) {
                        contents = result.contents.map(c =>
                            typeof c === 'string' ? { value: c } : { value: c.value || '' }
                        );
                    }

                    return {
                        contents: contents.map(c => ({ value: '```go\n' + c.value + '\n```' }))
                    };
                } catch (err) {
                    console.error('Hover error:', err);
                    return null;
                }
            }
        });

        // Definition provider
        monaco.languages.registerDefinitionProvider('go', {
            provideDefinition: async (model, position) => {
                if (!self.lspSocket || self.lspSocket.readyState !== WebSocket.OPEN) {
                    return null;
                }

                const filename = self.activeTab;
                if (!filename) return null;

                try {
                    const result = await self.sendLSPRequest('textDocument/definition', {
                        textDocument: { uri: self.getDocumentUri(filename) },
                        position: {
                            line: position.lineNumber - 1,
                            character: position.column - 1
                        }
                    });

                    if (!result) return null;

                    const locations = Array.isArray(result) ? result : [result];
                    return locations.map(loc => ({
                        uri: monaco.Uri.parse(loc.uri),
                        range: {
                            startLineNumber: loc.range.start.line + 1,
                            startColumn: loc.range.start.character + 1,
                            endLineNumber: loc.range.end.line + 1,
                            endColumn: loc.range.end.character + 1
                        }
                    }));
                } catch (err) {
                    console.error('Definition error:', err);
                    return null;
                }
            }
        });

        // Signature help provider
        monaco.languages.registerSignatureHelpProvider('go', {
            signatureHelpTriggerCharacters: ['(', ','],
            provideSignatureHelp: async (model, position) => {
                if (!self.lspSocket || self.lspSocket.readyState !== WebSocket.OPEN) {
                    return null;
                }

                const filename = self.activeTab;
                if (!filename) return null;

                try {
                    const result = await self.sendLSPRequest('textDocument/signatureHelp', {
                        textDocument: { uri: self.getDocumentUri(filename) },
                        position: {
                            line: position.lineNumber - 1,
                            character: position.column - 1
                        }
                    });

                    if (!result || !result.signatures) return null;

                    return {
                        value: {
                            signatures: result.signatures.map(sig => ({
                                label: sig.label,
                                documentation: sig.documentation,
                                parameters: (sig.parameters || []).map(p => ({
                                    label: p.label,
                                    documentation: p.documentation
                                }))
                            })),
                            activeSignature: result.activeSignature || 0,
                            activeParameter: result.activeParameter || 0
                        },
                        dispose: () => {}
                    };
                } catch (err) {
                    console.error('Signature help error:', err);
                    return null;
                }
            }
        });

        console.log('Go language providers registered');
    }

    lspCompletionKindToMonaco(kind) {
        const map = {
            1: monaco.languages.CompletionItemKind.Text,
            2: monaco.languages.CompletionItemKind.Method,
            3: monaco.languages.CompletionItemKind.Function,
            4: monaco.languages.CompletionItemKind.Constructor,
            5: monaco.languages.CompletionItemKind.Field,
            6: monaco.languages.CompletionItemKind.Variable,
            7: monaco.languages.CompletionItemKind.Class,
            8: monaco.languages.CompletionItemKind.Interface,
            9: monaco.languages.CompletionItemKind.Module,
            10: monaco.languages.CompletionItemKind.Property,
            11: monaco.languages.CompletionItemKind.Unit,
            12: monaco.languages.CompletionItemKind.Value,
            13: monaco.languages.CompletionItemKind.Enum,
            14: monaco.languages.CompletionItemKind.Keyword,
            15: monaco.languages.CompletionItemKind.Snippet,
            16: monaco.languages.CompletionItemKind.Color,
            17: monaco.languages.CompletionItemKind.File,
            18: monaco.languages.CompletionItemKind.Reference,
            19: monaco.languages.CompletionItemKind.Folder,
            20: monaco.languages.CompletionItemKind.EnumMember,
            21: monaco.languages.CompletionItemKind.Constant,
            22: monaco.languages.CompletionItemKind.Struct,
            23: monaco.languages.CompletionItemKind.Event,
            24: monaco.languages.CompletionItemKind.Operator,
            25: monaco.languages.CompletionItemKind.TypeParameter
        };
        return map[kind] || monaco.languages.CompletionItemKind.Text;
    }

    sendLSPNotification(method, params) {
        if (!this.lspSocket || this.lspSocket.readyState !== WebSocket.OPEN) {
            return;
        }

        const message = {
            jsonrpc: '2.0',
            method: method,
            params: params
        };

        this.lspSocket.send(JSON.stringify(message));
    }

    sendLSPInitialize() {
        const rootUri = 'file://' + (window.PROJECT_ROOT || '/');
        this.sendLSPRequest('initialize', {
            processId: null,
            rootUri: rootUri,
            rootPath: window.PROJECT_ROOT || '/',
            capabilities: {
                textDocument: {
                    completion: {
                        completionItem: {
                            snippetSupport: true,
                            documentationFormat: ['markdown', 'plaintext']
                        }
                    },
                    hover: {
                        contentFormat: ['markdown', 'plaintext']
                    },
                    definition: {},
                    references: {},
                    documentSymbol: {},
                    formatting: {},
                    publishDiagnostics: {
                        relatedInformation: true
                    }
                }
            }
        }).then(() => {
            this.sendLSPNotification('initialized', {});
            console.log('LSP initialized');
        }).catch(err => {
            console.error('LSP initialize failed:', err);
        });
    }

    handleLSPMessage(message) {
        if (message.id !== undefined) {
            // Response to a request
            const pending = this.lspPendingRequests.get(message.id);
            if (pending) {
                this.lspPendingRequests.delete(message.id);
                if (message.error) {
                    pending.reject(message.error);
                } else {
                    pending.resolve(message.result);
                }
            }
        } else if (message.method) {
            // Notification from server
            this.handleLSPNotification(message.method, message.params);
        }
    }

    handleLSPNotification(method, params) {
        switch (method) {
            case 'textDocument/publishDiagnostics':
                this.handleDiagnostics(params);
                break;
            default:
                console.log('LSP notification:', method, params);
        }
    }

    handleDiagnostics(params) {
        if (!this.monacoEditor) return;

        const model = this.monacoEditor.getModel();
        if (!model) return;

        // Convert LSP diagnostics to Monaco markers
        const markers = (params.diagnostics || []).map(diag => ({
            severity: this.lspSeverityToMonaco(diag.severity),
            startLineNumber: diag.range.start.line + 1,
            startColumn: diag.range.start.character + 1,
            endLineNumber: diag.range.end.line + 1,
            endColumn: diag.range.end.character + 1,
            message: diag.message,
            source: diag.source || 'gopls'
        }));

        monaco.editor.setModelMarkers(model, 'gopls', markers);

        // Update status bar with error/warning counts
        const errors = markers.filter(m => m.severity === monaco.MarkerSeverity.Error).length;
        const warnings = markers.filter(m => m.severity === monaco.MarkerSeverity.Warning).length;

        if (this.statusBar.errors) {
            this.statusBar.errors.textContent = errors;
            this.statusBar.errors.classList.toggle('hidden', errors === 0);
        }
        if (this.statusBar.warnings) {
            this.statusBar.warnings.textContent = warnings;
            this.statusBar.warnings.classList.toggle('hidden', warnings === 0);
        }
    }

    lspSeverityToMonaco(severity) {
        switch (severity) {
            case 1: return monaco.MarkerSeverity.Error;
            case 2: return monaco.MarkerSeverity.Warning;
            case 3: return monaco.MarkerSeverity.Info;
            case 4: return monaco.MarkerSeverity.Hint;
            default: return monaco.MarkerSeverity.Info;
        }
    }

    getDocumentUri(filename) {
        // Construct proper file URI
        const rootDir = window.PROJECT_ROOT || '';
        let fullPath = filename;
        if (!filename.startsWith('/') && rootDir) {
            fullPath = rootDir + '/' + filename;
        }
        return 'file://' + fullPath;
    }

    notifyLSPDocumentOpen(filename, content) {
        const languageId = filename.endsWith('.go') ? 'go' : 'plaintext';
        this.sendLSPNotification('textDocument/didOpen', {
            textDocument: {
                uri: this.getDocumentUri(filename),
                languageId: languageId,
                version: 1,
                text: content
            }
        });
    }

    notifyLSPDocumentChange(filename, content, version) {
        this.sendLSPNotification('textDocument/didChange', {
            textDocument: {
                uri: this.getDocumentUri(filename),
                version: version
            },
            contentChanges: [{ text: content }]
        });
    }

    notifyLSPDocumentClose(filename) {
        this.sendLSPNotification('textDocument/didClose', {
            textDocument: {
                uri: this.getDocumentUri(filename)
            }
        });
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

        // Context menu (Monaco handles its own context menu, but we keep this for compatibility)
        document.addEventListener('click', (e) => {
            this.hideContextMenu();
            this.hideAllMenus();
        });

        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => this.handleGlobalShortcuts(e));

        // File tree keyboard navigation
        if (this.fileTree) {
            this.fileTree.addEventListener('keydown', (e) => this.handleExplorerKeyDown(e));
        }

        // Window events
        window.addEventListener('beforeunload', (e) => this.onBeforeUnload(e));

        // File tree refresh
        document.getElementById('refreshBtn')?.addEventListener('click', () => this.loadFileTree());

        // Handle window resize for Monaco
        window.addEventListener('resize', () => {
            if (this.monacoEditor) {
                this.monacoEditor.layout();
            }
        });
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
    
    populateFileTree(files, container = null, basePath = '', level = 0) {
        const targetContainer = container || this.fileTree;

        // Only clear if this is the root level call
        if (!container) {
            targetContainer.innerHTML = '';
        }

        files.forEach(file => {
            const fileItem = document.createElement('div');
            fileItem.className = 'file-item';
            fileItem.dataset.itemType = 'file';
            fileItem.dataset.itemPath = basePath + file;
            fileItem.dataset.level = level;
            fileItem.tabIndex = 0; // Make focusable for keyboard navigation
            fileItem.style.paddingLeft = (8 + level * 16) + 'px';

            const icon = document.createElement('span');
            icon.className = 'file-icon';

            if (file.endsWith('/')) {
                const dirName = file.slice(0, -1);
                fileItem.classList.add('directory');
                fileItem.dataset.itemType = 'directory';
                fileItem.dataset.expanded = 'false';

                // Add expand/collapse arrow
                const arrow = document.createElement('span');
                arrow.className = 'dir-arrow';
                arrow.textContent = '▶';
                arrow.style.cssText = 'margin-right: 4px; font-size: 10px; display: inline-block; transition: transform 0.15s;';

                icon.classList.add('folder');

                // Create text node for directory name
                const nameSpan = document.createElement('span');
                nameSpan.textContent = dirName;

                fileItem.appendChild(arrow);
                fileItem.appendChild(icon);
                fileItem.appendChild(nameSpan);

                // Create children container (hidden initially)
                const childrenContainer = document.createElement('div');
                childrenContainer.className = 'dir-children';
                childrenContainer.style.display = 'none';
                childrenContainer.dataset.dirPath = basePath + file;

                // Add click handler for expanding/collapsing
                fileItem.addEventListener('click', async (e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    this.selectExplorerItem(fileItem);

                    const isExpanded = fileItem.dataset.expanded === 'true';

                    if (isExpanded) {
                        // Collapse
                        fileItem.dataset.expanded = 'false';
                        arrow.style.transform = 'rotate(0deg)';
                        childrenContainer.style.display = 'none';
                    } else {
                        // Expand
                        fileItem.dataset.expanded = 'true';
                        arrow.style.transform = 'rotate(90deg)';
                        childrenContainer.style.display = 'block';

                        // Load children if not already loaded
                        if (!childrenContainer.dataset.loaded) {
                            childrenContainer.innerHTML = '<div style="padding: 4px 8px; color: #888; font-size: 11px;">Loading...</div>';
                            await this.loadDirectoryContents(basePath + file, childrenContainer, level + 1);
                            childrenContainer.dataset.loaded = 'true';
                        }
                    }
                });

                targetContainer.appendChild(fileItem);
                targetContainer.appendChild(childrenContainer);
            } else {
                const ext = file.split('.').pop()?.toLowerCase() || 'txt';
                icon.classList.add(ext);

                // Create text node for file name
                const nameSpan = document.createElement('span');
                nameSpan.textContent = file;

                // Add spacer for alignment with directories (where arrow would be)
                const spacer = document.createElement('span');
                spacer.style.cssText = 'margin-right: 4px; width: 10px; display: inline-block;';

                fileItem.appendChild(spacer);
                fileItem.appendChild(icon);
                fileItem.appendChild(nameSpan);

                // Add selection and open functionality
                fileItem.addEventListener('click', (e) => {
                    e.preventDefault();
                    this.selectExplorerItem(fileItem);
                });
                fileItem.addEventListener('dblclick', (e) => {
                    e.preventDefault();
                    this.openFile(basePath + file);
                });

                targetContainer.appendChild(fileItem);
            }
        });
    }

    async loadDirectoryContents(dirPath, container, level) {
        try {
            const response = await fetch(`/api/list?dir=${encodeURIComponent(dirPath)}`);
            const result = await response.json();

            if (result.success) {
                container.innerHTML = '';
                // Filter out parent directory link
                const files = result.files.filter(f => f !== '../');

                if (files.length === 0) {
                    container.innerHTML = '<div style="padding: 4px 8px; padding-left: ' + (8 + level * 16) + 'px; color: #666; font-size: 11px; font-style: italic;">Empty directory</div>';
                } else {
                    this.populateFileTree(files, container, dirPath, level);
                }
            }
        } catch (error) {
            console.error('Error loading directory contents:', error);
            container.innerHTML = '<div style="padding: 4px 8px; color: #ff6b6b; font-size: 11px;">Error loading contents</div>';
        }
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

        // Create Monaco model for this file
        const language = this.getLanguageForFile(filename);
        const uri = monaco.Uri.parse('file://' + filename);
        let model = monaco.editor.getModel(uri);
        if (!model) {
            model = monaco.editor.createModel(content, language, uri);
        } else {
            model.setValue(content);
        }
        this.monacoModels.set(filename, model);

        // Store tab content
        this.openTabs.set(filename, {
            content,
            originalContent: content,
            modified: false,
            version: 1
        });

        // Notify LSP about opened document
        this.notifyLSPDocumentOpen(filename, content);

        this.switchTab(filename);
        this.updateUI();
    }

    getLanguageForFile(filename) {
        const ext = filename.split('.').pop()?.toLowerCase();
        const languageMap = {
            'go': 'go',
            'js': 'javascript',
            'ts': 'typescript',
            'json': 'json',
            'md': 'markdown',
            'css': 'css',
            'html': 'html',
            'htm': 'html',
            'yaml': 'yaml',
            'yml': 'yaml',
            'xml': 'xml',
            'sh': 'shell',
            'bash': 'shell',
            'py': 'python',
            'rs': 'rust',
            'c': 'c',
            'cpp': 'cpp',
            'h': 'c',
            'hpp': 'cpp'
        };
        return languageMap[ext] || 'plaintext';
    }

    switchTab(filename) {
        // Save current tab state
        if (this.activeTab && this.monacoEditor) {
            const tabData = this.openTabs.get(this.activeTab);
            if (tabData) {
                tabData.content = this.monacoEditor.getValue();
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

        // Switch to the Monaco model for this file
        const model = this.monacoModels.get(filename);
        if (model && this.monacoEditor) {
            this.monacoEditor.setModel(model);
            this.activeTab = filename;
            const tabData = this.openTabs.get(filename);
            if (tabData) {
                this.currentContent = tabData.content;
                this.isModified = tabData.modified;
            }
        }

        this.updateUI();
        this.updateStatusBar();
        if (this.monacoEditor) {
            this.monacoEditor.focus();
            // Restore breakpoints for this file
            this.updateBreakpointDecorations(filename);
        }
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

        // Dispose Monaco model
        const model = this.monacoModels.get(filename);
        if (model) {
            model.dispose();
            this.monacoModels.delete(filename);
        }

        // Notify LSP about closed document
        this.notifyLSPDocumentClose(filename);

        // Clean up breakpoints for this file
        this.breakpoints.delete(filename);
        this.breakpointDecorations.delete(filename);

        // Remove from open tabs
        this.openTabs.delete(filename);

        // Switch to another tab or show welcome screen
        if (this.activeTab === filename) {
            const remainingTabs = Array.from(this.openTabs.keys());
            if (remainingTabs.length > 0) {
                this.switchTab(remainingTabs[remainingTabs.length - 1]);
            } else {
                this.activeTab = null;
                if (this.monacoEditor) {
                    this.monacoEditor.setModel(null);
                }
                this.updateUI();
            }
        }
    }

    // Toggle breakpoint at the specified line
    toggleBreakpoint(filename, lineNumber) {
        if (!this.breakpoints.has(filename)) {
            this.breakpoints.set(filename, new Set());
        }

        const fileBreakpoints = this.breakpoints.get(filename);

        if (fileBreakpoints.has(lineNumber)) {
            // Remove breakpoint
            fileBreakpoints.delete(lineNumber);
            console.log(`🔴 Removed breakpoint at ${filename}:${lineNumber}`);
        } else {
            // Add breakpoint
            fileBreakpoints.add(lineNumber);
            console.log(`🔴 Added breakpoint at ${filename}:${lineNumber}`);
        }

        // Update decorations
        this.updateBreakpointDecorations(filename);
    }

    // Update breakpoint decorations in the editor
    updateBreakpointDecorations(filename) {
        if (!this.monacoEditor || this.activeTab !== filename) {
            return;
        }

        const fileBreakpoints = this.breakpoints.get(filename) || new Set();
        const decorations = [];

        fileBreakpoints.forEach(lineNumber => {
            decorations.push({
                range: new monaco.Range(lineNumber, 1, lineNumber, 1),
                options: {
                    isWholeLine: true,
                    glyphMarginClassName: 'breakpoint-decoration',
                    className: 'breakpoint-line',
                    glyphMarginHoverMessage: { value: `Breakpoint at line ${lineNumber}` }
                }
            });
        });

        // Get old decorations and replace them
        const oldDecorations = this.breakpointDecorations.get(filename) || [];
        const newDecorations = this.monacoEditor.deltaDecorations(oldDecorations, decorations);
        this.breakpointDecorations.set(filename, newDecorations);
    }

    // Get all breakpoints for a file
    getBreakpoints(filename) {
        return Array.from(this.breakpoints.get(filename) || []).sort((a, b) => a - b);
    }

    // Get all breakpoints across all files
    getAllBreakpoints() {
        const allBreakpoints = {};
        this.breakpoints.forEach((lines, filename) => {
            if (lines.size > 0) {
                allBreakpoints[filename] = Array.from(lines).sort((a, b) => a - b);
            }
        });
        return allBreakpoints;
    }

    // Clear all breakpoints for a file
    clearBreakpoints(filename) {
        if (this.breakpoints.has(filename)) {
            this.breakpoints.get(filename).clear();
            this.updateBreakpointDecorations(filename);
            console.log(`🔴 Cleared all breakpoints in ${filename}`);
        }
    }

    // Clear all breakpoints across all files
    clearAllBreakpoints() {
        this.breakpoints.forEach((_, filename) => {
            this.breakpoints.get(filename).clear();
            if (this.activeTab === filename) {
                this.updateBreakpointDecorations(filename);
            }
        });
        console.log('🔴 Cleared all breakpoints');
    }

    onEditorChange() {
        if (this.activeTab && this.monacoEditor) {
            const tabData = this.openTabs.get(this.activeTab);
            if (tabData) {
                tabData.content = this.monacoEditor.getValue();
                tabData.modified = tabData.content !== tabData.originalContent;
                tabData.version = (tabData.version || 0) + 1;

                // Update tab visual state
                const tab = document.querySelector(`[data-filename="${this.activeTab}"]`);
                if (tab) {
                    if (tabData.modified) {
                        tab.classList.add('modified');
                    } else {
                        tab.classList.remove('modified');
                    }
                }

                // Notify LSP about document change
                this.notifyLSPDocumentChange(this.activeTab, tabData.content, tabData.version);
            }
        }

        this.updateStatusBar();
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

        if (!this.monacoEditor) {
            this.setStatus('Editor not initialized', 'error');
            return;
        }

        try {
            this.setStatus('Saving...', 'info');

            const content = this.monacoEditor.getValue();
            const response = await fetch('/api/save', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    filename: this.activeTab,
                    content: content
                })
            });

            const result = await response.json();

            if (result.success) {
                const tabData = this.openTabs.get(this.activeTab);
                if (tabData) {
                    tabData.originalContent = content;
                    tabData.modified = false;

                    const tab = document.querySelector(`[data-filename="${this.activeTab}"]`);
                    if (tab) {
                        tab.classList.remove('modified');
                    }
                }

                // Notify LSP about the save (some language servers use this)
                this.sendLSPNotification('textDocument/didSave', {
                    textDocument: {
                        uri: this.getDocumentUri(this.activeTab)
                    }
                });

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
            // Trigger Monaco layout update
            if (this.monacoEditor) {
                this.monacoEditor.layout();
            }
        } else {
            this.noEditorMessage.classList.remove('hidden');
            this.editorContainer.classList.add('hidden');
        }
    }

    updateStatusBar() {
        if (!this.activeTab || !this.monacoEditor) return;

        // Get cursor position from Monaco
        const position = this.monacoEditor.getPosition();
        if (position) {
            this.statusBar.selection.textContent = `Ln ${position.lineNumber}, Col ${position.column}`;
        }

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
                'txt': 'Plain Text',
                'py': 'Python',
                'rs': 'Rust',
                'c': 'C',
                'cpp': 'C++',
                'yaml': 'YAML',
                'yml': 'YAML'
            };
            this.statusBar.fileType.textContent = fileTypes[ext] || 'Plain Text';
        }

        // Selection info if text is selected
        const selection = this.monacoEditor.getSelection();
        if (selection && !selection.isEmpty()) {
            const model = this.monacoEditor.getModel();
            if (model) {
                const selectedText = model.getValueInRange(selection);
                const selectedLines = selectedText.split('\n').length;
                const selectedChars = selectedText.length;

                if (selectedLines > 1) {
                    this.statusBar.selection.textContent += ` (${selectedLines} lines, ${selectedChars} chars selected)`;
                } else {
                    this.statusBar.selection.textContent += ` (${selectedChars} chars selected)`;
                }
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

// Context menu functions - Monaco handles these via its built-in commands
function cutText() {
    if (window.codeEditor?.monacoEditor) {
        window.codeEditor.monacoEditor.focus();
        window.codeEditor.monacoEditor.trigger('keyboard', 'editor.action.clipboardCutAction');
    }
}

function copyText() {
    if (window.codeEditor?.monacoEditor) {
        window.codeEditor.monacoEditor.focus();
        window.codeEditor.monacoEditor.trigger('keyboard', 'editor.action.clipboardCopyAction');
    }
}

function pasteText() {
    if (window.codeEditor?.monacoEditor) {
        window.codeEditor.monacoEditor.focus();
        window.codeEditor.monacoEditor.trigger('keyboard', 'editor.action.clipboardPasteAction');
    }
}

function selectAllText() {
    if (window.codeEditor?.monacoEditor) {
        const model = window.codeEditor.monacoEditor.getModel();
        if (model) {
            const fullRange = model.getFullModelRange();
            window.codeEditor.monacoEditor.setSelection(fullRange);
            window.codeEditor.monacoEditor.focus();
        }
    }
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
    if (filename && window.codeEditor?.activeTab && window.codeEditor?.monacoEditor) {
        const editor = window.codeEditor;
        const content = editor.monacoEditor.getValue();

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
    if (window.codeEditor?.monacoEditor) {
        window.codeEditor.monacoEditor.trigger('keyboard', 'undo');
    }
}

function redoAction() {
    if (window.codeEditor?.monacoEditor) {
        window.codeEditor.monacoEditor.trigger('keyboard', 'redo');
    }
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

    // Hide any previous selection toolbar
    hideSelectionToolbar();

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
                    <div style="display: flex; align-items: center; justify-content: space-between;">
                        <span>⚙️ FUNCTIONS</span>
                        <button onclick="loadFilesIntoExplorer()" style="padding: 2px 6px; background: #007acc; color: white; border: none; border-radius: 3px; cursor: pointer; font-size: 10px;">
                            ← Back
                        </button>
                    </div>
                `;
                fileTree.appendChild(header);

                // Clear any previous function selections
                selectedFunctionItems.clear();
                
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
                        // Parse function name from the line (format: "funcName" or "package.funcName")
                        const funcName = trimmedLine.split('(')[0].trim();
                        const funcData = { name: funcName, fullSignature: trimmedLine };

                        // Try to parse function information
                        const functionItem = document.createElement('div');
                        functionItem.className = 'explorer-item function-item';
                        functionItem.tabIndex = 0; // Make focusable for keyboard navigation

                        // Add checkbox for selection
                        const checkbox = document.createElement('input');
                        checkbox.type = 'checkbox';
                        checkbox.className = 'function-checkbox';
                        checkbox.funcRef = funcData; // Store reference to function data
                        checkbox.addEventListener('click', (e) => {
                            e.stopPropagation(); // Prevent item selection when clicking checkbox
                        });
                        checkbox.addEventListener('change', (e) => {
                            if (e.target.checked) {
                                selectedFunctionItems.add(funcData);
                                functionItem.style.backgroundColor = 'rgba(0, 122, 204, 0.2)';
                            } else {
                                selectedFunctionItems.delete(funcData);
                                functionItem.style.backgroundColor = '';
                            }
                            updateFunctionSelectionCount();
                            console.log('⚙️ Selected functions:', selectedFunctionItems.size);
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
    // Hide any previous selection toolbar
    hideSelectionToolbar();

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
            <div style="display: flex; align-items: center; justify-content: space-between;">
                <span>📊 Static Call Graph</span>
                <button onclick="loadFilesIntoExplorer()" style="padding: 2px 6px; background: #007acc; color: white; border: none; border-radius: 3px; cursor: pointer; font-size: 10px;">
                    ← Back
                </button>
            </div>
        `;
        fileTree.appendChild(header);

        // Clear previous selections when showing new call graph
        selectedCallGraphItems.clear();
        
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

// Track selected call graph items (multi-select)
let selectedCallGraphItems = new Set();

// Track selected function items (multi-select for Functions view)
let selectedFunctionItems = new Set();

// Track current selection context ('functions' or 'callgraph')
let currentSelectionContext = null;

// Update the main toolbar selection controls
function updateSelectionToolbar(count, context, contextLabel) {
    const toolbar = document.getElementById('selectionToolbar');
    const contextDisplay = document.getElementById('selectionContext');

    if (count > 0) {
        currentSelectionContext = context;
        if (toolbar) {
            toolbar.style.display = 'flex';
        }
        if (contextDisplay) {
            contextDisplay.textContent = `${contextLabel}: ${count} selected`;
        }
    } else {
        // Only hide if this context was active
        if (currentSelectionContext === context) {
            currentSelectionContext = null;
            if (toolbar) {
                toolbar.style.display = 'none';
            }
        }
    }
}

// Generate hooks from current selection (toolbar button handler)
function generateHooksFromSelection() {
    if (currentSelectionContext === 'functions') {
        generateHooksFromFunctions();
    } else if (currentSelectionContext === 'callgraph') {
        generateHooksFile();
    }
}

// Show/hide progress indicator on Generate Hooks button
function setGenerateHooksProgress(isLoading) {
    const toolbar = document.getElementById('selectionToolbar');
    const generateBtn = toolbar?.querySelector('.toolbar-button-success');

    if (generateBtn) {
        if (isLoading) {
            generateBtn.disabled = true;
            generateBtn.dataset.originalHtml = generateBtn.innerHTML;
            generateBtn.innerHTML = `
                <svg class="spinner" width="16" height="16" viewBox="0 0 16 16" style="animation: spin 1s linear infinite;">
                    <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="2" fill="none" stroke-dasharray="28" stroke-dashoffset="8"/>
                </svg>
                <span style="margin-left: 4px;">Generating...</span>
            `;
        } else {
            generateBtn.disabled = false;
            if (generateBtn.dataset.originalHtml) {
                generateBtn.innerHTML = generateBtn.dataset.originalHtml;
            }
        }
    }
}

// Select all items based on current context (toolbar button handler)
function selectAllFromToolbar() {
    if (currentSelectionContext === 'functions' || document.querySelector('.function-checkbox')) {
        selectAllFunctionItems();
    } else if (currentSelectionContext === 'callgraph' || document.querySelector('.call-graph-checkbox')) {
        selectAllCallGraphItems();
    }
}

// Clear selection based on current context (toolbar button handler)
function clearSelectionFromToolbar() {
    if (currentSelectionContext === 'functions') {
        clearFunctionSelection();
    } else if (currentSelectionContext === 'callgraph') {
        clearCallGraphSelection();
    }
}

function renderCallTree(container, nodes, level = 0) {
    nodes.forEach(node => {
        const nodeItem = document.createElement('div');
        nodeItem.className = 'call-graph-item';
        nodeItem.style.paddingLeft = (level * 16) + 'px';

        const nodeContent = document.createElement('div');
        nodeContent.className = 'call-graph-content';
        nodeContent.style.cssText = `
            display: flex;
            align-items: center;
            padding: 4px 8px;
            cursor: pointer;
            border-radius: 3px;
            margin: 1px 0;
            transition: background-color 0.15s;
        `;

        // Add checkbox for multi-selection
        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'call-graph-checkbox';
        checkbox.style.cssText = `
            margin-right: 8px;
            cursor: pointer;
            width: 14px;
            height: 14px;
            accent-color: #007acc;
        `;
        // Store node reference on checkbox for Select All functionality
        checkbox.nodeRef = node;
        checkbox.addEventListener('click', (e) => {
            e.stopPropagation();
            if (checkbox.checked) {
                selectedCallGraphItems.add(node);
                nodeContent.style.backgroundColor = 'rgba(0, 122, 204, 0.2)';
            } else {
                selectedCallGraphItems.delete(node);
                nodeContent.style.backgroundColor = '';
            }
            updateCallGraphSelectionCount();
            console.log('📊 Selected items:', selectedCallGraphItems.size);
        });

        // Add expand/collapse arrow if has children
        const arrow = document.createElement('span');
        arrow.className = 'call-graph-arrow';
        arrow.style.cssText = `
            margin-right: 6px;
            width: 14px;
            display: inline-block;
            font-size: 10px;
            text-align: center;
            color: #888;
        `;

        if (node.children.length > 0) {
            arrow.textContent = node.expanded ? '▼' : '▶';
            arrow.style.cursor = 'pointer';
        } else {
            arrow.textContent = '•';
            arrow.style.color = '#555';
        }

        // Add function icon
        const icon = document.createElement('span');
        icon.style.cssText = 'margin-right: 6px; font-size: 12px;';
        icon.textContent = node.isRoot ? '🔵' : '⚡';

        // Add function name
        const nameSpan = document.createElement('span');
        nameSpan.className = 'call-graph-func-name';
        nameSpan.textContent = node.name;
        nameSpan.style.cssText = `
            color: ${node.isRoot ? '#4fc3f7' : '#9cdcfe'};
            font-weight: ${node.isRoot ? 'bold' : 'normal'};
            font-family: 'Consolas', 'Courier New', monospace;
            font-size: 13px;
        `;

        // Add line numbers if present
        let linesSpan = null;
        if (node.lines && !node.isRoot) {
            linesSpan = document.createElement('span');
            linesSpan.className = 'call-graph-lines';
            linesSpan.textContent = ` (line${node.lines.includes(',') ? 's' : ''} ${node.lines})`;
            linesSpan.style.cssText = `
                color: #6a9955;
                font-size: 11px;
                margin-left: 6px;
                font-family: 'Consolas', 'Courier New', monospace;
            `;
        }

        nodeContent.appendChild(checkbox);
        nodeContent.appendChild(arrow);
        nodeContent.appendChild(icon);
        nodeContent.appendChild(nameSpan);
        if (linesSpan) {
            nodeContent.appendChild(linesSpan);
        }

        // Add hover effects
        nodeContent.addEventListener('mouseenter', () => {
            if (!checkbox.checked) {
                nodeContent.style.backgroundColor = 'rgba(255, 255, 255, 0.05)';
            }
        });
        nodeContent.addEventListener('mouseleave', () => {
            if (!checkbox.checked) {
                nodeContent.style.backgroundColor = '';
            }
        });

        // Add click handler for navigation and expand/collapse (not for selection - use checkbox)
        nodeContent.addEventListener('click', (e) => {
            // Don't handle if clicking on checkbox
            if (e.target === checkbox) return;

            e.stopPropagation();

            // Try to navigate to the function if we can find it
            if (node.lines && !node.isRoot) {
                const lineNum = parseInt(node.lines.split(',')[0].trim());
                if (!isNaN(lineNum)) {
                    // Try to find and open the file containing this function
                    tryNavigateToFunction(node.name, lineNum);
                }
            }

            // Toggle expand/collapse if has children
            if (node.children.length > 0) {
                node.expanded = !node.expanded;
                arrow.textContent = node.expanded ? '▼' : '▶';

                const childrenContainer = nodeItem.querySelector('.call-graph-children');
                if (childrenContainer) {
                    childrenContainer.style.display = node.expanded ? 'block' : 'none';
                }
            }
        });

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

// Update selection count display and show/hide Generate Hooks button
function updateCallGraphSelectionCount() {
    const count = selectedCallGraphItems.size;

    // Update main toolbar
    updateSelectionToolbar(count, 'callgraph', 'Static Call Graph');
}

// Get selected call graph items
function getSelectedCallGraphItems() {
    return Array.from(selectedCallGraphItems);
}

// Clear all selections
function clearCallGraphSelection() {
    selectedCallGraphItems.clear();
    document.querySelectorAll('.call-graph-checkbox').forEach(cb => {
        cb.checked = false;
        cb.closest('.call-graph-content').style.backgroundColor = '';
    });
    updateCallGraphSelectionCount();
}

// Select all visible items
function selectAllCallGraphItems() {
    document.querySelectorAll('.call-graph-checkbox').forEach(cb => {
        cb.checked = true;
        cb.closest('.call-graph-content').style.backgroundColor = 'rgba(0, 122, 204, 0.2)';
        // Add node to selection set using stored reference
        if (cb.nodeRef) {
            selectedCallGraphItems.add(cb.nodeRef);
        }
    });
    updateCallGraphSelectionCount();
}

// ============================================
// Functions View Selection Helper Functions
// ============================================

// Update the function selection count display
function updateFunctionSelectionCount() {
    const count = selectedFunctionItems.size;

    // Update main toolbar
    updateSelectionToolbar(count, 'functions', 'Functions');
}

// Get selected function items
function getSelectedFunctionItems() {
    return Array.from(selectedFunctionItems);
}

// Clear all function selections
function clearFunctionSelection() {
    selectedFunctionItems.clear();
    document.querySelectorAll('.function-checkbox').forEach(cb => {
        cb.checked = false;
        cb.closest('.function-item').style.backgroundColor = '';
    });
    updateFunctionSelectionCount();
}

// Select all visible function items
function selectAllFunctionItems() {
    document.querySelectorAll('.function-checkbox').forEach(cb => {
        cb.checked = true;
        cb.closest('.function-item').style.backgroundColor = 'rgba(0, 122, 204, 0.2)';
        // Add function to selection set using stored reference
        if (cb.funcRef) {
            selectedFunctionItems.add(cb.funcRef);
        }
    });
    updateFunctionSelectionCount();
}

// Generate hooks file from selected functions (Functions view)
async function generateHooksFromFunctions() {
    const selectedItems = getSelectedFunctionItems();
    if (selectedItems.length === 0) {
        alert('Please select at least one function to generate hooks.');
        return;
    }

    console.log('🔧 Generating hooks for', selectedItems.length, 'functions:', selectedItems.map(f => f.name));

    // Show progress indicator
    setGenerateHooksProgress(true);

    // Get unique function names (avoid duplicates)
    const functionNames = [...new Set(selectedItems.map(item => item.name))];

    // Generate the hooks file content
    const hooksContent = generateHooksCode(functionNames);

    // Directory name for the hooks module
    const dirName = 'generated_hooks';

    try {
        // Call backend API to create the hooks module
        const response = await fetch('/api/create-hooks-module', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                dirName: dirName,
                fileContent: hooksContent
            })
        });

        const result = await response.json();

        if (result.success) {
            console.log('✅ Created hooks module:', result.directory);
            console.log('📝 Hooks file:', result.hooksFile);
            console.log('📦 Module name:', result.moduleName);

            // Open the generated file in the editor (but don't switch side panel)
            if (window.codeEditor) {
                const filename = result.hooksFile;
                const content = hooksContent;

                // Check if tab already exists
                if (window.codeEditor.openTabs.has(filename)) {
                    // Close the existing tab first, then reopen with new content
                    const model = window.codeEditor.monacoModels.get(filename);
                    if (model) {
                        model.dispose();
                        window.codeEditor.monacoModels.delete(filename);
                    }
                    window.codeEditor.openTabs.delete(filename);

                    // Remove the tab element
                    const tabElement = document.querySelector(`[data-filename="${filename}"]`);
                    if (tabElement) {
                        tabElement.remove();
                    }
                }

                // Create new tab with the generated content
                window.codeEditor.createOrSwitchTab(filename, content);
                console.log('✅ Opened/updated generated hooks file in editor');
            }

            // Show success message (non-blocking notification style)
            const notification = document.createElement('div');
            notification.className = 'hooks-notification';
            notification.innerHTML = `
                <div style="background: #4caf50; color: white; padding: 10px 15px; border-radius: 4px; margin-bottom: 10px; box-shadow: 0 2px 8px rgba(0,0,0,0.3);">
                    <strong>✅ Hooks module created!</strong><br>
                    <small>${result.hooksFile}</small>
                </div>
            `;
            notification.style.cssText = 'position: fixed; top: 60px; right: 20px; z-index: 10000;';
            document.body.appendChild(notification);

            // Auto-remove notification after 4 seconds
            setTimeout(() => {
                notification.remove();
            }, 4000);
        } else {
            console.error('❌ Failed to create hooks module:', result.error);
            alert('Failed to create hooks module: ' + result.error);
        }
    } catch (error) {
        console.error('❌ Error creating hooks module:', error);
        alert('Error creating hooks module: ' + error.message);
    } finally {
        // Hide progress indicator
        setGenerateHooksProgress(false);
    }
}

// Generate hooks file from selected call graph functions
async function generateHooksFile() {
    const selectedItems = getSelectedCallGraphItems();
    if (selectedItems.length === 0) {
        alert('Please select at least one function to generate hooks.');
        return;
    }

    console.log('🔧 Generating hooks for', selectedItems.length, 'functions:', selectedItems.map(n => n.name));

    // Show progress indicator
    setGenerateHooksProgress(true);

    // Get unique function names (avoid duplicates)
    const functionNames = [...new Set(selectedItems.map(item => item.name))];

    // Generate the hooks file content
    const hooksContent = generateHooksCode(functionNames);

    // Directory name for the hooks module
    const dirName = 'generated_hooks';

    try {
        // Call backend API to create the hooks module
        const response = await fetch('/api/create-hooks-module', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                dirName: dirName,
                fileContent: hooksContent
            })
        });

        const result = await response.json();

        if (result.success) {
            console.log('✅ Created hooks module:', result.directory);
            console.log('📝 Hooks file:', result.hooksFile);
            console.log('📦 Module name:', result.moduleName);

            // Open the generated file in the editor (but don't switch side panel)
            // Use the content we already generated instead of fetching from server
            if (window.codeEditor) {
                const filename = result.hooksFile;
                const content = hooksContent; // Use the content we already have

                // Check if tab already exists
                if (window.codeEditor.openTabs.has(filename)) {
                    // Close the existing tab first, then reopen with new content
                    // This ensures a clean state
                    const model = window.codeEditor.monacoModels.get(filename);
                    if (model) {
                        model.dispose();
                        window.codeEditor.monacoModels.delete(filename);
                    }
                    window.codeEditor.openTabs.delete(filename);

                    // Remove the tab element
                    const tabElement = document.querySelector(`[data-filename="${filename}"]`);
                    if (tabElement) {
                        tabElement.remove();
                    }
                }

                // Create new tab with the generated content
                window.codeEditor.createOrSwitchTab(filename, content);
                console.log('✅ Opened/updated generated hooks file in editor');
            }

            // Show success message (non-blocking notification style)
            const notification = document.createElement('div');
            notification.className = 'hooks-notification';
            notification.innerHTML = `
                <div style="background: #4caf50; color: white; padding: 10px 15px; border-radius: 4px; margin-bottom: 10px; box-shadow: 0 2px 8px rgba(0,0,0,0.3);">
                    <strong>✅ Hooks module created!</strong><br>
                    <small>${result.hooksFile}</small>
                </div>
            `;
            notification.style.cssText = 'position: fixed; top: 60px; right: 20px; z-index: 10000;';
            document.body.appendChild(notification);

            // Auto-remove notification after 4 seconds
            setTimeout(() => {
                notification.remove();
            }, 4000);
        } else {
            console.error('❌ Failed to create hooks module:', result.error);
            alert('Failed to create hooks module: ' + result.error);
        }
    } catch (error) {
        console.error('❌ Error creating hooks module:', error);
        alert('Error creating hooks module: ' + error.message);
    } finally {
        // Hide progress indicator
        setGenerateHooksProgress(false);
    }
}

// Generate Go hooks code for the given function names
function generateHooksCode(functionNames, moduleName = '') {
    // Convert function names to PascalCase for hook function names
    const toPascalCase = (name) => {
        return name.charAt(0).toUpperCase() + name.slice(1);
    };

    // Use a placeholder that will be replaced based on the actual module
    const hooksModulePath = moduleName || 'generated_hooks';

    // Generate hook definitions for ProvideHooks()
    const hookDefinitions = functionNames.map(funcName => {
        const pascalName = toPascalCase(funcName);
        return `		{
			Target: hooks.InjectTarget{
				Package:  "main",
				Function: "${funcName}",
				Receiver: "",
			},
			Hooks: &hooks.InjectFunctions{
				Before: "Before${pascalName}",
				After:  "After${pascalName}",
				From:   "${hooksModulePath}",
			},
		},`;
    }).join('\n');

    // Generate Before/After hook implementations with go:linkname support
    const hookImplementations = functionNames.map(funcName => {
        const pascalName = toPascalCase(funcName);
        return `// Before${pascalName} is called before ${funcName}() executes
// The HookContext allows passing data to the After hook and skipping the original call
func Before${pascalName}(ctx HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// After${pascalName} is called after ${funcName}() completes
func After${pascalName}(ctx HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}`;
    }).join('\n\n');

    // Build the complete hooks file with minimal HookContext interface
    const code = `package generated_hooks

import (
	"fmt"
	"time"
	_ "unsafe" // Required for go:linkname

	"github.com/pdelewski/go-build-interceptor/hooks"
)

// ============================================================================
// Minimal HookContext Interface
// ============================================================================

// HookContext provides a minimal interface for hook functions.
// It allows passing data between Before and After hooks, and optionally
// skipping the original function call.
type HookContext interface {
	// SetData stores arbitrary data to pass from Before to After hook
	SetData(data interface{})
	// GetData retrieves data stored by SetData
	GetData() interface{}
	// SetKeyData stores a key-value pair
	SetKeyData(key string, val interface{})
	// GetKeyData retrieves a value by key
	GetKeyData(key string) interface{}
	// HasKeyData checks if a key exists
	HasKeyData(key string) bool
	// SetSkipCall when true, skips the original function call
	SetSkipCall(skip bool)
	// IsSkipCall returns whether to skip the original call
	IsSkipCall() bool
	// GetFuncName returns the name of the hooked function
	GetFuncName() string
	// GetPackageName returns the package name of the hooked function
	GetPackageName() string
}

// ============================================================================
// Hook Provider (for go-build-interceptor)
// ============================================================================

// GeneratedHookProvider implements the HookProvider interface
type GeneratedHookProvider struct{}

// ProvideHooks returns the hook definitions for the selected functions
func (h *GeneratedHookProvider) ProvideHooks() []*hooks.Hook {
	return []*hooks.Hook{
${hookDefinitions}
	}
}

// Ensure GeneratedHookProvider implements the HookProvider interface
var _ hooks.HookProvider = (*GeneratedHookProvider)(nil)

// ============================================================================
// Hook Implementations
// ============================================================================

${hookImplementations}

// ============================================================================
// go:linkname Usage (for reference)
// ============================================================================
//
// When go-build-interceptor injects hooks into the target code, it should
// generate go:linkname declarations to call these hooks. Example:
//
// In the TARGET package (e.g., main), the injected code would look like:
//
//     import _ "unsafe" // required for go:linkname
//
//     //go:linkname BeforeMyFunc generated_hooks.BeforeMyFunc
//     func BeforeMyFunc(ctx HookContext)
//
//     //go:linkname AfterMyFunc generated_hooks.AfterMyFunc
//     func AfterMyFunc(ctx HookContext)
//
//     func MyFunc() {
//         ctx := &HookContextImpl{funcName: "MyFunc", packageName: "main"}
//         BeforeMyFunc(ctx)
//         if !ctx.IsSkipCall() {
//             defer AfterMyFunc(ctx)
//             // original function body...
//         }
//     }
//
// This allows the hooks to be defined in this separate module while being
// called from the instrumented target code.
`;

    return code;
}

// Try to navigate to a function by looking up the Functions view data
async function tryNavigateToFunction(funcName, lineNum) {
    try {
        // First, check if we have an open file that might contain this function
        console.log(`🔍 Looking for function: ${funcName}`);

        // Try to fetch pack-functions to find the file
        const response = await fetch('/api/pack-functions');
        if (!response.ok) return;

        const data = await response.json();
        const functionsData = data.content;

        // Parse functions data to find the file containing this function
        const lines = functionsData.split('\n');
        let currentFile = '';

        for (const line of lines) {
            // Check for file line (ends with .go:)
            if (line.match(/^\S+\.go:$/)) {
                currentFile = line.slice(0, -1); // Remove trailing colon
            }
            // Check if this line contains our function
            else if (line.includes(funcName)) {
                // Extract just the function name from the line for comparison
                const match = line.match(/^\s*(?:func\s+)?(?:\([^)]+\)\s+)?(\w+)/);
                if (match && match[1] === funcName && currentFile) {
                    console.log(`📂 Found ${funcName} in ${currentFile}`);

                    // Open the file and try to go to the line
                    if (window.codeEditor) {
                        await window.codeEditor.openFile(currentFile);
                        // Give the editor time to load, then scroll to line
                        setTimeout(() => {
                            scrollToLine(lineNum);
                        }, 200);
                    }
                    return;
                }
            }
        }

        console.log(`⚠️ Could not find file for function: ${funcName}`);
    } catch (error) {
        console.error('Error navigating to function:', error);
    }
}

// Scroll editor to a specific line
function scrollToLine(lineNum) {
    const editor = document.getElementById('editor');
    if (!editor) return;

    const lines = editor.value.split('\n');
    if (lineNum > lines.length) return;

    // Calculate character position for the target line
    let charPos = 0;
    for (let i = 0; i < lineNum - 1; i++) {
        charPos += lines[i].length + 1; // +1 for newline
    }

    // Set cursor position and scroll
    editor.focus();
    editor.setSelectionRange(charPos, charPos + lines[lineNum - 1].length);

    // Scroll to make the line visible
    const lineHeight = parseInt(getComputedStyle(editor).lineHeight) || 20;
    editor.scrollTop = (lineNum - 5) * lineHeight; // Show some context above

    console.log(`📍 Scrolled to line ${lineNum}`);
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
            padding: 4px 0;
            overflow-y: auto;
            max-height: calc(100vh - 150px);
        `;

        // Parse work directory output and create clickable file items
        // Format is hierarchical with indentation:
        // 📁 b001/
        //   📄 file.go (123 bytes)
        //   📁 src/
        //     📄 main.go (456 bytes)
        const lines = workDirData.split('\n');
        let workBasePath = '';
        let dirStack = []; // Stack to track nested directories based on indentation

        lines.forEach(line => {
            if (!line.trim()) return;

            // Check for WORK= anywhere in the line (e.g., "First command: WORK=/path...")
            if (line.includes('WORK=')) {
                const workMatch = line.match(/WORK=([^\s]+)/);
                if (workMatch) {
                    workBasePath = workMatch[1].trim();
                    console.log('📁 Extracted WORK path:', workBasePath);
                }

                // Show the WORK path as a header
                const workHeader = document.createElement('div');
                workHeader.style.cssText = `
                    padding: 8px 16px;
                    font-family: 'Consolas', 'Courier New', monospace;
                    font-size: 12px;
                    color: var(--vscode-text-muted);
                    background: var(--vscode-sidebar-bg);
                    border-bottom: 1px solid var(--vscode-border);
                `;
                workHeader.innerHTML = `<span style="color: #4fc3f7;">WORK=</span>${workBasePath}`;
                content.appendChild(workHeader);
                return;
            }

            // Skip header lines
            if (line.includes('Contents of work directory') || line.includes('===') ||
                line.includes('Parsed ') || line.includes('Work Directory Mode') ||
                line.includes('Found WORK directory')) {
                return;
            }

            // Calculate indentation level (number of leading spaces / 2)
            const leadingSpaces = line.match(/^(\s*)/)[1].length;
            const indentLevel = Math.floor(leadingSpaces / 2);
            const trimmedLine = line.trim();

            // Check if it's a directory line (has 📁 and ends with /)
            const dirMatch = trimmedLine.match(/^📁\s*(.+)\/$/);
            if (dirMatch) {
                const dirName = dirMatch[1];

                // Adjust directory stack to current level
                while (dirStack.length > indentLevel) {
                    dirStack.pop();
                }
                dirStack.push(dirName);

                // Show directory as a section header
                const dirHeader = document.createElement('div');
                const paddingLeft = 16 + (indentLevel * 16);
                dirHeader.style.cssText = `
                    padding: 6px 16px 6px ${paddingLeft}px;
                    font-family: 'Consolas', 'Courier New', monospace;
                    font-size: 12px;
                    color: #dcdcaa;
                    background: rgba(255, 255, 255, 0.03);
                    margin-top: 2px;
                `;
                dirHeader.innerHTML = `📁 ${dirName}/`;
                content.appendChild(dirHeader);
                return;
            }

            // Check if it's a file line (has 📄 and size in bytes)
            const fileMatch = trimmedLine.match(/^📄\s*(.+?)\s*\((\d+)\s*bytes?\)$/);
            if (fileMatch) {
                const fileName = fileMatch[1];
                const fileSize = fileMatch[2];

                // Adjust directory stack to current level
                while (dirStack.length > indentLevel) {
                    dirStack.pop();
                }

                // Build full path from work base + directory stack + filename
                let fullPath = workBasePath;
                if (dirStack.length > 0) {
                    fullPath += '/' + dirStack.join('/');
                }
                fullPath += '/' + fileName;

                const fileItem = document.createElement('div');
                fileItem.className = 'explorer-item file-item';
                const paddingLeft = 32 + (indentLevel * 16);
                fileItem.style.cssText = `
                    padding: 4px 16px 4px ${paddingLeft}px;
                    cursor: pointer;
                    font-family: 'Consolas', 'Courier New', monospace;
                    font-size: 13px;
                `;

                // Get file extension for icon
                const ext = fileName.split('.').pop().toLowerCase();
                let icon = '📄';
                if (ext === 'go') icon = '🐹';
                else if (ext === 'js') icon = '📜';
                else if (ext === 'json') icon = '📋';
                else if (ext === 'md') icon = '📝';
                else if (ext === 'css') icon = '🎨';
                else if (ext === 'html') icon = '🌐';
                else if (ext === 'a') icon = '📦';
                else if (ext === 'o') icon = '⚙️';
                else if (ext === 'h') icon = '📋';

                fileItem.innerHTML = `<span style="margin-right: 8px;">${icon}</span>${fileName} <span style="color: #666; font-size: 11px;">(${fileSize} bytes)</span>`;
                fileItem.title = fullPath; // Show full path on hover

                console.log(`📄 File: ${fileName}, Full path: ${fullPath}`);

                // Add click handler to open file
                fileItem.addEventListener('click', () => {
                    if (window.codeEditor) {
                        console.log('📂 Opening file:', fullPath);
                        window.codeEditor.openFile(fullPath);
                    }
                });

                // Add hover effect
                fileItem.addEventListener('mouseenter', () => {
                    fileItem.style.background = 'var(--vscode-hover)';
                });
                fileItem.addEventListener('mouseleave', () => {
                    fileItem.style.background = '';
                });

                content.appendChild(fileItem);
            }
        });

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
    // Clear any selections and hide toolbar when going back to file explorer
    hideSelectionToolbar();
    window.codeEditor?.switchSidePanel('explorer');
    window.codeEditor?.loadFileTree();
}

// Hide the selection toolbar and clear all selections
function hideSelectionToolbar() {
    const toolbar = document.getElementById('selectionToolbar');
    if (toolbar) {
        toolbar.style.display = 'none';
    }
    currentSelectionContext = null;
    selectedFunctionItems.clear();
    selectedCallGraphItems.clear();
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
    const hooksFile = prompt('Enter hooks file path:', './generated_hooks/generated_hooks.go');

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