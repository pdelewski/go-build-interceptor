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
        const absolutePath = filename.startsWith('/') ? filename : `${window.PROJECT_ROOT}/${filename}`;

        if (fileBreakpoints.has(lineNumber)) {
            // Remove breakpoint
            fileBreakpoints.delete(lineNumber);
            console.log(`🔴 Removed breakpoint at ${filename}:${lineNumber}`);

            // If debugging, tell dlv to remove the breakpoint
            if (typeof isDebugging !== 'undefined' && isDebugging) {
                const key = `${absolutePath}:${lineNumber}`;
                const bpId = breakpointIds.get(key);
                if (bpId) {
                    sendDebugCommand({ command: 'clearBreakpoint', id: bpId });
                    breakpointIds.delete(key);
                }
            }
        } else {
            // Add breakpoint
            fileBreakpoints.add(lineNumber);
            console.log(`🔴 Added breakpoint at ${filename}:${lineNumber}`);

            // If debugging, tell dlv to add the breakpoint
            if (typeof isDebugging !== 'undefined' && isDebugging) {
                console.log(`Sending breakpoint to dlv: ${absolutePath}:${lineNumber}`);
                sendDebugCommand({
                    command: 'setBreakpoint',
                    file: absolutePath,
                    line: lineNumber
                });
            }
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
                        // Check if this is metadata/info line (no checkbox)
                        const isMetadata = trimmedLine.startsWith('Parsed ') ||
                                          trimmedLine.startsWith('===') ||
                                          trimmedLine.startsWith('Found ') ||
                                          trimmedLine.startsWith('File:') ||
                                          trimmedLine.match(/^\/.*\.go:$/); // File path lines like "/path/to/file.go:"

                        if (isMetadata) {
                            // Display metadata without checkbox - use indentation for file paths
                            const metaItem = document.createElement('div');
                            metaItem.className = 'explorer-item metadata-item';
                            const isFilePath = trimmedLine.match(/^\/.*\.go:$/) || trimmedLine.startsWith('File:');
                            metaItem.style.cssText = isFilePath
                                ? 'padding: 6px 8px 4px 8px; color: #dcdcaa; font-weight: bold; cursor: default; margin-top: 8px;'
                                : 'padding: 4px 8px 4px 12px; color: #888; font-style: italic; cursor: default;';
                            metaItem.textContent = trimmedLine;
                            fileTree.appendChild(metaItem);
                        } else {
                            // Parse function name from the line (format: "funcName" or "package.funcName")
                            const funcName = trimmedLine.split('(')[0].trim();
                            const funcData = { name: funcName, fullSignature: trimmedLine };

                            // Try to parse function information
                            const functionItem = document.createElement('div');
                            functionItem.className = 'explorer-item function-item';
                            functionItem.tabIndex = 0; // Make focusable for keyboard navigation
                            functionItem.style.paddingLeft = '20px'; // Indent functions under file headers

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
    // Uses hooks.HookContext from the hooks package
    const hookImplementations = functionNames.map(funcName => {
        const pascalName = toPascalCase(funcName);
        return `// Before${pascalName} is called before ${funcName}() executes
// The HookContext allows passing data to the After hook and skipping the original call
func Before${pascalName}(ctx hooks.HookContext) {
	ctx.SetKeyData("startTime", time.Now())
	fmt.Printf("[BEFORE] %s.%s()\\n", ctx.GetPackageName(), ctx.GetFuncName())
}

// After${pascalName} is called after ${funcName}() completes
func After${pascalName}(ctx hooks.HookContext) {
	if startTime, ok := ctx.GetKeyData("startTime").(time.Time); ok {
		duration := time.Since(startTime)
		fmt.Printf("[AFTER] %s.%s() completed in %v\\n", ctx.GetPackageName(), ctx.GetFuncName(), duration)
	}
}`;
    }).join('\n\n');

    // Build the hooks file using the hooks package for types
    const code = `package generated_hooks

import (
	"fmt"
	"time"
	_ "unsafe" // Required for go:linkname

	"github.com/pdelewski/go-build-interceptor/hooks"
)

// ============================================================================
// Hook Provider (for go-build-interceptor parsing)
// ============================================================================

// ProvideHooks returns the hook definitions for the selected functions
func ProvideHooks() []*hooks.Hook {
	return []*hooks.Hook{
${hookDefinitions}
	}
}

// ============================================================================
// Hook Implementations
// ============================================================================
// These functions are called via go:linkname from the instrumented code.
// The instrumented code generates trampoline functions that link to these.

${hookImplementations}
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
                        // Check if this is metadata/info line (no checkbox)
                        const isMetadata = trimmedLine.startsWith('Parsed ') ||
                                          trimmedLine.startsWith('===') ||
                                          trimmedLine.startsWith('Found ') ||
                                          trimmedLine.startsWith('File:');

                        if (isMetadata) {
                            // Display metadata without checkbox
                            const metaItem = document.createElement('div');
                            metaItem.className = 'explorer-item metadata-item';
                            metaItem.style.cssText = 'padding: 4px 8px 4px 12px; color: #888; font-style: italic; cursor: default;';
                            metaItem.textContent = trimmedLine;
                            fileTree.appendChild(metaItem);
                        } else {
                            // Display package with checkbox
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
    const monacoEditor = window.codeEditor?.monacoEditor;
    if (monacoEditor) {
        const currentSize = monacoEditor.getOption(monaco.editor.EditorOption.fontSize);
        const newSize = currentSize + 2;
        monacoEditor.updateOptions({ fontSize: newSize });
    }
}

function zoomOut() {
    const monacoEditor = window.codeEditor?.monacoEditor;
    if (monacoEditor) {
        const currentSize = monacoEditor.getOption(monaco.editor.EditorOption.fontSize);
        if (currentSize > 8) {
            const newSize = currentSize - 2;
            monacoEditor.updateOptions({ fontSize: newSize });
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

// File selector dialog for selecting hooks files - with directory navigation
function showFileSelector(defaultPath = './generated_hooks/generated_hooks.go') {
    return new Promise(async (resolve) => {
        let currentDir = '.';
        const selectedFiles = new Set(); // Track selected files across directories

        // Remove existing dialog if present
        const existing = document.getElementById('fileSelectorDialog');
        if (existing) {
            existing.remove();
        }

        // Create overlay
        const overlay = document.createElement('div');
        overlay.id = 'fileSelectorOverlay';
        overlay.className = 'file-selector-overlay';

        // Create dialog
        const dialog = document.createElement('div');
        dialog.id = 'fileSelectorDialog';
        dialog.className = 'file-selector-dialog';

        // Header
        const header = document.createElement('div');
        header.className = 'file-selector-header';
        header.innerHTML = '<span>Select Hooks Files for Compile</span>';

        // Path bar showing current directory
        const pathBar = document.createElement('div');
        pathBar.className = 'file-selector-path';
        pathBar.id = 'fileSelectorPath';

        // Action buttons (Select All / Deselect All)
        const actionBar = document.createElement('div');
        actionBar.className = 'file-selector-actions';
        actionBar.innerHTML = `
            <button class="file-selector-btn" id="selectAllBtn">Select All .go</button>
            <button class="file-selector-btn" id="deselectAllBtn">Deselect All</button>
            <span class="file-selector-count" id="selectedCount">0 selected</span>
        `;

        // File list container
        const fileList = document.createElement('div');
        fileList.className = 'file-selector-list';
        fileList.id = 'fileSelectorList';

        // Footer with OK/Cancel
        const footer = document.createElement('div');
        footer.className = 'file-selector-footer';
        footer.innerHTML = `
            <button class="file-selector-btn file-selector-cancel" id="cancelBtn">Cancel</button>
            <button class="file-selector-btn file-selector-ok" id="okBtn">Compile Selected</button>
        `;

        // Assemble dialog
        dialog.appendChild(header);
        dialog.appendChild(pathBar);
        dialog.appendChild(actionBar);
        dialog.appendChild(fileList);
        dialog.appendChild(footer);
        overlay.appendChild(dialog);
        document.body.appendChild(overlay);

        // Update selected count display
        function updateSelectedCount() {
            const countEl = document.getElementById('selectedCount');
            if (countEl) {
                countEl.textContent = `${selectedFiles.size} selected`;
            }
        }

        // Load directory contents
        async function loadDirectory(dir) {
            currentDir = dir;
            const pathEl = document.getElementById('fileSelectorPath');
            const listEl = document.getElementById('fileSelectorList');

            pathEl.innerHTML = `<span class="path-icon">📁</span> ${dir === '.' ? 'Current Directory' : dir}`;
            listEl.innerHTML = '<div class="file-selector-loading">Loading...</div>';

            try {
                const response = await fetch(`/api/list?dir=${encodeURIComponent(dir)}`);
                const result = await response.json();

                if (result.success) {
                    renderDirectory(result.files, dir);
                } else {
                    listEl.innerHTML = `<div class="file-selector-error">Error: ${result.error}</div>`;
                }
            } catch (e) {
                console.error('Failed to load directory:', e);
                listEl.innerHTML = `<div class="file-selector-error">Failed to load directory</div>`;
            }
        }

        // Render selected files section at the top
        function renderSelectedFiles() {
            let selectedSection = document.getElementById('selectedFilesSection');

            if (selectedFiles.size === 0) {
                if (selectedSection) {
                    selectedSection.remove();
                }
                return;
            }

            if (!selectedSection) {
                selectedSection = document.createElement('div');
                selectedSection.id = 'selectedFilesSection';
                selectedSection.className = 'selected-files-section';
                const listEl = document.getElementById('fileSelectorList');
                listEl.parentNode.insertBefore(selectedSection, listEl);
            }

            selectedSection.innerHTML = '<div class="selected-files-header">Selected Files:</div>';

            Array.from(selectedFiles).sort().forEach(filePath => {
                const item = document.createElement('div');
                item.className = 'selected-file-item';

                const removeBtn = document.createElement('span');
                removeBtn.className = 'selected-file-remove';
                removeBtn.textContent = '✕';
                removeBtn.title = 'Remove';
                removeBtn.addEventListener('click', () => {
                    selectedFiles.delete(filePath);
                    renderSelectedFiles();
                    updateSelectedCount();
                    // Update checkbox if visible in current directory
                    const listEl = document.getElementById('fileSelectorList');
                    listEl.querySelectorAll('.file-selector-item').forEach(itemEl => {
                        const nameEl = itemEl.querySelector('.file-selector-name');
                        const cb = itemEl.querySelector('.file-selector-checkbox');
                        if (nameEl && cb && nameEl.title === filePath) {
                            cb.checked = false;
                        }
                    });
                });

                const nameSpan = document.createElement('span');
                nameSpan.className = 'selected-file-name';
                nameSpan.textContent = filePath;
                nameSpan.title = filePath;

                item.appendChild(removeBtn);
                item.appendChild(nameSpan);
                selectedSection.appendChild(item);
            });
        }

        // Render directory contents
        function renderDirectory(files, dir) {
            const listEl = document.getElementById('fileSelectorList');
            listEl.innerHTML = '';

            // Filter: only directories and .go files, exclude "../" from API response
            const filteredFiles = files.filter(file => {
                if (file === '../') return false; // We'll add ".." manually
                const isDirectory = file.endsWith('/');
                if (isDirectory) return true;
                return file.endsWith('.go');
            });

            // Always add ".." at the top to navigate up (except at root)
            const allFiles = ['../', ...filteredFiles];

            allFiles.forEach(file => {
                const isDirectory = file.endsWith('/');
                const fileName = isDirectory ? file.slice(0, -1) : file;
                const fullPath = dir === '.' ? fileName : `${dir}/${fileName}`;

                const item = document.createElement('div');
                item.className = 'file-selector-item';

                if (isDirectory) {
                    // Directory item - clickable to navigate
                    item.classList.add('directory');
                    item.innerHTML = `
                        <span class="file-selector-icon">📁</span>
                        <span class="file-selector-name">${fileName}</span>
                    `;
                    item.addEventListener('click', () => {
                        if (fileName === '..') {
                            // Go up one level - append .. to current path
                            if (dir === '.') {
                                loadDirectory('..');
                            } else {
                                // Append /.. to go up from current directory
                                loadDirectory(dir + '/..');
                            }
                        } else {
                            loadDirectory(fullPath);
                        }
                    });
                } else {
                    // .go file item with checkbox
                    const checkbox = document.createElement('input');
                    checkbox.type = 'checkbox';
                    checkbox.className = 'file-selector-checkbox';
                    checkbox.checked = selectedFiles.has(fullPath);
                    checkbox.addEventListener('change', (e) => {
                        if (e.target.checked) {
                            selectedFiles.add(fullPath);
                        } else {
                            selectedFiles.delete(fullPath);
                        }
                        renderSelectedFiles();
                        updateSelectedCount();
                    });

                    item.appendChild(checkbox);

                    const iconSpan = document.createElement('span');
                    iconSpan.className = 'file-selector-icon';
                    iconSpan.textContent = '🐹';
                    item.appendChild(iconSpan);

                    const nameSpan = document.createElement('span');
                    nameSpan.className = 'file-selector-name';
                    nameSpan.textContent = fileName;
                    nameSpan.title = fullPath;
                    item.appendChild(nameSpan);

                    // Click on file row (not checkbox) to toggle
                    item.style.cursor = 'pointer';
                    item.addEventListener('click', (e) => {
                        if (e.target.type !== 'checkbox') {
                            const cb = item.querySelector('.file-selector-checkbox');
                            if (cb) {
                                cb.checked = !cb.checked;
                                if (cb.checked) {
                                    selectedFiles.add(fullPath);
                                } else {
                                    selectedFiles.delete(fullPath);
                                }
                                renderSelectedFiles();
                                updateSelectedCount();
                            }
                        }
                    });
                }

                listEl.appendChild(item);
            });

            renderSelectedFiles();
            updateSelectedCount();
        }

        // Event handlers
        document.getElementById('selectAllBtn').addEventListener('click', () => {
            const listEl = document.getElementById('fileSelectorList');
            listEl.querySelectorAll('.file-selector-checkbox').forEach(cb => {
                cb.checked = true;
                selectedFiles.add(cb.closest('.file-selector-item').querySelector('.file-selector-name').title ||
                    (currentDir === '.' ? '' : currentDir + '/') + cb.closest('.file-selector-item').querySelector('.file-selector-name').textContent);
            });
            // Re-add with correct paths
            listEl.querySelectorAll('.file-selector-item:not(.directory)').forEach(item => {
                const nameEl = item.querySelector('.file-selector-name');
                const cb = item.querySelector('.file-selector-checkbox');
                if (cb && nameEl) {
                    const fullPath = nameEl.title || (currentDir === '.' ? nameEl.textContent : `${currentDir}/${nameEl.textContent}`);
                    if (cb.checked) {
                        selectedFiles.add(fullPath);
                    }
                }
            });
            renderSelectedFiles();
            updateSelectedCount();
        });

        document.getElementById('deselectAllBtn').addEventListener('click', () => {
            const listEl = document.getElementById('fileSelectorList');
            listEl.querySelectorAll('.file-selector-checkbox').forEach(cb => {
                cb.checked = false;
            });
            // Only remove files from current directory view
            listEl.querySelectorAll('.file-selector-item:not(.directory)').forEach(item => {
                const nameEl = item.querySelector('.file-selector-name');
                if (nameEl) {
                    const fullPath = nameEl.title || (currentDir === '.' ? nameEl.textContent : `${currentDir}/${nameEl.textContent}`);
                    selectedFiles.delete(fullPath);
                }
            });
            renderSelectedFiles();
            updateSelectedCount();
        });

        document.getElementById('cancelBtn').addEventListener('click', () => {
            overlay.remove();
            resolve(null);
        });

        document.getElementById('okBtn').addEventListener('click', () => {
            overlay.remove();
            resolve(selectedFiles.size > 0 ? Array.from(selectedFiles).join(',') : null);
        });

        // Close on overlay click
        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) {
                overlay.remove();
                resolve(null);
            }
        });

        // Load initial directory
        loadDirectory('.');
    });
}

// Update the runCompile function to show output in terminal
async function runCompile() {
    const hooksFile = await showFileSelector('./generated_hooks/generated_hooks.go');

    if (!hooksFile || hooksFile.trim() === '') {
        return;
    }

    // Show terminal and clear previous output
    showTerminal();
    clearTerminal();
    addTerminalOutput('$ go-build-interceptor --compile ' + hooksFile.trim(), 'terminal-command');
    addTerminalOutput('Compiling...', 'terminal-info');

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
            addTerminalOutput('', '');
            addTerminalOutput('❌ Compile Failed:', 'terminal-error');
            addTerminalOutput(data.error, 'terminal-error');
        } else {
            const output = data.content || 'Compile completed successfully (no output)';
            addTerminalOutput('', '');
            output.split('\n').forEach(line => {
                addTerminalOutput(line, '');
            });
            addTerminalOutput('', '');
            addTerminalOutput('✅ Compile completed successfully', 'terminal-success');
        }
    } catch (err) {
        console.error('Compile error:', err);
        addTerminalOutput('', '');
        addTerminalOutput('❌ Error: ' + err.message, 'terminal-error');
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

    // Show terminal and clear previous output
    showTerminal();
    clearTerminal();
    addTerminalOutput('$ ' + execPath.trim(), 'terminal-command');
    addTerminalOutput('Running...', 'terminal-info');

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
            addTerminalOutput('', '');
            addTerminalOutput('❌ Execution Failed:', 'terminal-error');
            addTerminalOutput(data.error, 'terminal-error');
        } else {
            const output = data.content || 'No output';
            addTerminalOutput('', '');
            output.split('\n').forEach(line => {
                addTerminalOutput(line, '');
            });
            addTerminalOutput('', '');
            addTerminalOutput('✅ Execution completed', 'terminal-success');
        }
    } catch (err) {
        console.error('Execution error:', err);
        addTerminalOutput('', '');
        addTerminalOutput('❌ Error: ' + err.message, 'terminal-error');
    }
}

// ============================================
// Debug Session Management
// ============================================

let debugSocket = null;
let debugPort = 2345;
let isDebugging = false;
let currentLineDecoration = [];
let breakpointIds = new Map(); // file:line -> dlv breakpoint id

// Run the debugger with Delve
async function runDebug() {
    const execPath = prompt('Enter executable path to debug (e.g., ./hello or hello):');

    if (!execPath || execPath.trim() === '') {
        return;
    }

    const portStr = prompt('Enter debug port (default: 2345):', '2345');
    debugPort = parseInt(portStr) || 2345;

    // Show loading message
    updateDebugStatus('Starting...');

    try {
        // First, start dlv via the API
        const response = await fetch('/api/debug', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                executablePath: execPath.trim(),
                port: debugPort
            })
        });

        const data = await response.json();

        if (data.error) {
            showMessageWindow('Debug Failed', data.error, 'error');
            updateDebugStatus('Failed');
            return;
        }

        if (data.success) {
            // Wait a moment for dlv to fully start
            await new Promise(resolve => setTimeout(resolve, 1000));

            // Connect to the debug WebSocket
            connectDebugWebSocket(debugPort);
        }
    } catch (err) {
        console.error('Debug error:', err);
        showMessageWindow('Debug Error', 'Failed to start debugger: ' + err.message, 'error');
        updateDebugStatus('Error');
    }
}

function connectDebugWebSocket(port) {
    const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    debugSocket = new WebSocket(`${wsProtocol}//${window.location.host}/ws/debug?port=${port}`);

    debugSocket.onopen = () => {
        console.log('Debug WebSocket connected');
        isDebugging = true;
        document.getElementById('debugToolbar').style.display = 'flex';
        showDebugPanel(); // Show the debug panel
        updateDebugStatus('Connected - Setting breakpoints...');

        // Send all existing breakpoints to dlv
        sendExistingBreakpoints();

        // Request initial state after a short delay
        setTimeout(() => {
            sendDebugCommand({ command: 'state' });
            updateDebugStatus('Ready - Click Continue (F5) to start');
        }, 500);
    };

    debugSocket.onmessage = (event) => {
        try {
            const msg = JSON.parse(event.data);
            handleDebugMessage(msg);
        } catch (err) {
            console.error('Error parsing debug message:', err);
        }
    };

    debugSocket.onclose = () => {
        console.log('Debug WebSocket closed');
        endDebugSession();
    };

    debugSocket.onerror = (err) => {
        console.error('Debug WebSocket error:', err);
        showMessageWindow('Debug Error', 'WebSocket connection failed', 'error');
        endDebugSession();
    };
}

function handleDebugMessage(msg) {
    console.log('Debug message received:', JSON.stringify(msg, null, 2));

    if (msg.type === 'error') {
        showMessageWindow('Debug Error', msg.error, 'error');
        return;
    }

    if (msg.type === 'response') {
        // Parse the result
        let result = msg.result;
        if (typeof result === 'string') {
            try {
                result = JSON.parse(result);
            } catch (e) {
                console.log('Could not parse result as JSON:', result);
            }
        }

        console.log('Parsed result:', result);

        if (!result) {
            console.log('No result in response');
            return;
        }

        // Check for state information (dlv Command responses have State)
        if (result.State) {
            console.log('Found State in result');
            updateDebugState(result.State);
        }
        // State method returns state directly
        else if (result.Running !== undefined || result.CurrentThread || result.SelectedGoroutine) {
            console.log('Result appears to be state directly');
            updateDebugState(result);
        }

        // Check for breakpoint creation response
        if (result.Breakpoint) {
            const bp = result.Breakpoint;
            const key = `${bp.file || bp.File}:${bp.line || bp.Line}`;
            const bpId = bp.id || bp.ID;
            console.log(`Breakpoint created: ${key} -> id=${bpId}`);
            breakpointIds.set(key, bpId);
        }

        // Check for local variables response (ListLocalVars)
        if (result.Variables !== undefined && msg.method === 'RPCServer.ListLocalVars') {
            console.log('Received local variables:', result.Variables);
            updateVariablesDisplay(result.Variables, false);
        }

        // Check for function arguments response (ListFunctionArgs)
        if (result.Args !== undefined || (result.Variables !== undefined && msg.method === 'RPCServer.ListFunctionArgs')) {
            const args = result.Args || result.Variables;
            console.log('Received function arguments:', args);
            updateVariablesDisplay(args, true);
        }

        // Check for stacktrace response
        if (result.Locations !== undefined) {
            console.log('Received stack trace:', result.Locations);
            updateCallStackDisplay(result.Locations);
        }
    }
}

function updateDebugState(state) {
    if (!state) return;

    console.log('Debug state:', JSON.stringify(state, null, 2));

    if (state.exited || state.Exited) {
        updateDebugStatus('Program exited');
        clearCurrentLineHighlight();
        clearDebugPanelContent();
        return;
    }

    if (state.Running) {
        updateDebugStatus('Running...');
        clearCurrentLineHighlight();
        clearDebugPanelContent();
        return;
    }

    // Get current location - try multiple sources (dlv uses capitalized field names)
    let loc = null;
    let funcName = 'unknown';

    // Try SelectedGoroutine first (dlv's field name)
    if (state.SelectedGoroutine && state.SelectedGoroutine.currentLoc) {
        loc = state.SelectedGoroutine.currentLoc;
    } else if (state.selectedGoroutine && state.selectedGoroutine.currentLoc) {
        loc = state.selectedGoroutine.currentLoc;
    }
    // Fall back to CurrentThread
    else if (state.CurrentThread) {
        loc = {
            file: state.CurrentThread.file,
            line: state.CurrentThread.line,
            function: state.CurrentThread.function
        };
    } else if (state.currentThread) {
        loc = {
            file: state.currentThread.file,
            line: state.currentThread.line,
            function: state.currentThread.function
        };
    }

    if (loc && loc.file && loc.line) {
        const file = loc.file;
        const line = loc.line;
        funcName = loc.function ? (loc.function.name || loc.function.Name || 'unknown') : 'unknown';

        updateDebugStatus(`Paused at ${funcName} (line ${line})`);

        // Highlight current line
        highlightCurrentLine(file, line);

        // Request variables and call stack when paused
        clearDebugPanelContent();
        requestDebugInfo();
    }
}

function highlightCurrentLine(file, line) {
    // Clear previous highlight
    clearCurrentLineHighlight();

    const editor = window.codeEditor?.monacoEditor;
    if (!editor) return;

    // Check if we need to open a different file
    const currentFile = window.codeEditor?.activeTab;
    const targetFileName = file.split('/').pop();
    const currentFileName = currentFile ? currentFile.split('/').pop() : null;

    // If the target file is different from the currently open file, open it
    if (currentFileName !== targetFileName) {
        console.log(`📂 Debug stepping to different file: ${file} (current: ${currentFile || 'none'})`);
        // Open the file - this will switch the tab and update the editor
        window.codeEditor?.openFile(file).then(() => {
            // After file is opened, apply the decoration
            const newEditor = window.codeEditor?.monacoEditor;
            if (newEditor) {
                currentLineDecoration = newEditor.deltaDecorations(currentLineDecoration, [{
                    range: new monaco.Range(line, 1, line, 1),
                    options: {
                        isWholeLine: true,
                        className: 'current-line-decoration',
                        glyphMarginClassName: 'current-line-glyph'
                    }
                }]);
                newEditor.revealLineInCenter(line);
            }
        }).catch(e => {
            console.error('Failed to open file for debug:', e);
        });
        return;
    }

    // Same file - just update the decoration
    currentLineDecoration = editor.deltaDecorations(currentLineDecoration, [{
        range: new monaco.Range(line, 1, line, 1),
        options: {
            isWholeLine: true,
            className: 'current-line-decoration',
            glyphMarginClassName: 'current-line-glyph'
        }
    }]);

    // Scroll to the line
    editor.revealLineInCenter(line);
}

function clearCurrentLineHighlight() {
    const editor = window.codeEditor?.monacoEditor;
    if (editor && currentLineDecoration.length > 0) {
        currentLineDecoration = editor.deltaDecorations(currentLineDecoration, []);
    }
}

function sendDebugCommand(cmd) {
    if (debugSocket && debugSocket.readyState === WebSocket.OPEN) {
        console.log('Sending debug command:', cmd);
        debugSocket.send(JSON.stringify(cmd));
    } else {
        console.warn('Debug socket not connected');
    }
}

// Send all existing breakpoints to dlv when debug session starts
function sendExistingBreakpoints() {
    console.log('Sending existing breakpoints to dlv...');

    const breakpoints = window.codeEditor?.breakpoints;
    if (!breakpoints) {
        console.log('No breakpoints map found');
        return;
    }

    for (const [filePath, lineSet] of breakpoints) {
        for (const line of lineSet) {
            // Build absolute file path
            const absolutePath = filePath.startsWith('/') ? filePath : `${window.PROJECT_ROOT}/${filePath}`;
            console.log(`Setting breakpoint: ${absolutePath}:${line}`);

            sendDebugCommand({
                command: 'setBreakpoint',
                file: absolutePath,
                line: line
            });
        }
    }
}

function updateDebugStatus(status) {
    const statusEl = document.getElementById('debugStatus');
    if (statusEl) {
        statusEl.textContent = status;
    }
}

// Debug control functions
function debugContinue() {
    updateDebugStatus('Running...');
    clearCurrentLineHighlight();
    sendDebugCommand({ command: 'continue' });
}

function debugStepOver() {
    updateDebugStatus('Stepping...');
    sendDebugCommand({ command: 'next' });
}

function debugStepInto() {
    updateDebugStatus('Stepping...');
    sendDebugCommand({ command: 'step' });
}

function debugStepOut() {
    updateDebugStatus('Stepping...');
    sendDebugCommand({ command: 'stepOut' });
}

function debugStop() {
    sendDebugCommand({ command: 'stop' });
    endDebugSession();
}

function endDebugSession() {
    isDebugging = false;
    document.getElementById('debugToolbar').style.display = 'none';
    hideDebugPanel(); // Hide the debug panel
    clearCurrentLineHighlight();

    if (debugSocket) {
        debugSocket.close();
        debugSocket = null;
    }

    updateDebugStatus('Stopped');
}

// ============================================
// Keyboard Shortcuts for Debug
// ============================================

document.addEventListener('keydown', (e) => {
    if (!isDebugging) return;

    if (e.key === 'F5' && !e.shiftKey) {
        e.preventDefault();
        debugContinue();
    } else if (e.key === 'F10') {
        e.preventDefault();
        debugStepOver();
    } else if (e.key === 'F11' && !e.shiftKey) {
        e.preventDefault();
        debugStepInto();
    } else if (e.key === 'F11' && e.shiftKey) {
        e.preventDefault();
        debugStepOut();
    } else if (e.key === 'F5' && e.shiftKey) {
        e.preventDefault();
        debugStop();
    }
});

// ============================================
// Debug Panel - Variables and Call Stack
// ============================================

// Toggle debug section collapse state
function toggleDebugSection(sectionId) {
    const section = document.getElementById(sectionId);
    if (section) {
        section.classList.toggle('collapsed');
    }
}

// Show the debug panel
function showDebugPanel() {
    const panel = document.getElementById('debugPanel');
    if (panel) {
        panel.classList.add('visible');
    }
}

// Hide the debug panel
function hideDebugPanel() {
    const panel = document.getElementById('debugPanel');
    if (panel) {
        panel.classList.remove('visible');
    }
    // Clear content
    clearDebugPanelContent();
}

// Clear debug panel content
function clearDebugPanelContent() {
    const varsContent = document.getElementById('variablesContent');
    const stackContent = document.getElementById('callStackContent');
    const varsCount = document.getElementById('variablesCount');
    const stackCount = document.getElementById('callStackCount');

    if (varsContent) varsContent.innerHTML = '<div class="debug-empty-state">No variables to display</div>';
    if (stackContent) stackContent.innerHTML = '<div class="debug-empty-state">No call stack to display</div>';
    if (varsCount) varsCount.textContent = '0';
    if (stackCount) stackCount.textContent = '0';
}

// Request variables and stack trace from debugger
function requestDebugInfo() {
    if (!isDebugging || !debugSocket || debugSocket.readyState !== WebSocket.OPEN) {
        return;
    }

    // Request local variables
    sendDebugCommand({ command: 'listLocalVars' });

    // Request function arguments
    sendDebugCommand({ command: 'listFunctionArgs' });

    // Request stack trace
    sendDebugCommand({ command: 'stacktrace' });
}

// Update variables display
function updateVariablesDisplay(variables, isArgs = false) {
    const varsContent = document.getElementById('variablesContent');
    const varsCount = document.getElementById('variablesCount');
    if (!varsContent) return;

    // If this is the first update, clear the empty state
    if (varsContent.querySelector('.debug-empty-state')) {
        varsContent.innerHTML = '';
    }

    // Add variables to the display
    if (Array.isArray(variables) && variables.length > 0) {
        variables.forEach(v => {
            const varEl = createVariableElement(v);
            varsContent.appendChild(varEl);
        });

        // Update count
        if (varsCount) {
            const currentCount = parseInt(varsCount.textContent) || 0;
            varsCount.textContent = currentCount + variables.length;
        }
    }
}

// Create a DOM element for a variable
function createVariableElement(variable) {
    const div = document.createElement('div');
    div.className = 'debug-variable';

    const name = variable.name || variable.Name || 'unknown';
    const type = variable.type || variable.Type || '';
    const value = formatVariableValue(variable);
    const valueClass = getValueClass(variable);

    div.innerHTML = `
        <span class="debug-variable-name">${escapeHtml(name)}</span>
        <span class="debug-variable-type">${escapeHtml(type)}</span>
        <span class="debug-variable-value ${valueClass}">${escapeHtml(value)}</span>
    `;

    return div;
}

// Format variable value for display
function formatVariableValue(variable) {
    const value = variable.value || variable.Value;
    const kind = variable.kind || variable.Kind;

    if (value === undefined || value === null) {
        return 'nil';
    }

    // Handle string values
    if (typeof value === 'string') {
        // Check for pointer/address values
        if (value.startsWith('0x') || value.startsWith('*')) {
            return value;
        }
        return value;
    }

    // Handle numeric values
    if (typeof value === 'number') {
        return value.toString();
    }

    // Handle boolean values
    if (typeof value === 'boolean') {
        return value.toString();
    }

    // Handle objects (structs, etc.)
    if (typeof value === 'object') {
        return JSON.stringify(value);
    }

    return String(value);
}

// Get CSS class for value type
function getValueClass(variable) {
    const kind = variable.kind || variable.Kind;
    const value = variable.value || variable.Value;

    // Numeric kinds in dlv: int, int8-64, uint, uint8-64, float32, float64, complex64, complex128
    if (kind >= 2 && kind <= 16) {
        return 'number';
    }

    // Boolean kind
    if (kind === 1) {
        return 'bool';
    }

    // Nil/invalid
    if (value === undefined || value === null || value === 'nil') {
        return 'nil';
    }

    return '';
}

// Update call stack display
function updateCallStackDisplay(frames) {
    const stackContent = document.getElementById('callStackContent');
    const stackCount = document.getElementById('callStackCount');
    if (!stackContent) return;

    stackContent.innerHTML = '';

    if (!Array.isArray(frames) || frames.length === 0) {
        stackContent.innerHTML = '<div class="debug-empty-state">No call stack to display</div>';
        if (stackCount) stackCount.textContent = '0';
        return;
    }

    frames.forEach((frame, index) => {
        const frameEl = createStackFrameElement(frame, index);
        stackContent.appendChild(frameEl);
    });

    if (stackCount) {
        stackCount.textContent = frames.length;
    }
}

// Create a DOM element for a stack frame
function createStackFrameElement(frame, index) {
    const div = document.createElement('div');
    div.className = 'debug-stack-frame' + (index === 0 ? ' active' : '');

    const funcName = frame.function?.name || frame.Function?.Name || 'unknown';
    const file = frame.file || frame.File || '';
    const line = frame.line || frame.Line || 0;
    const fileName = file.split('/').pop();

    div.innerHTML = `
        <span class="debug-stack-frame-function">${escapeHtml(funcName)}</span>
        <span class="debug-stack-frame-location">
            <span class="file">${escapeHtml(fileName)}</span>:<span class="line">${line}</span>
        </span>
    `;

    // Click to navigate to frame location
    div.addEventListener('click', () => {
        if (file && line) {
            highlightCurrentLine(file, line);
        }
    });

    return div;
}

// Escape HTML to prevent XSS
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}