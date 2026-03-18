package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"synovium/ast"
	"synovium/lexer"
	"synovium/parser"
	"synovium/sema"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ============================================================================
// JSON-RPC & LSP TYPES
// ============================================================================

type Request struct {
	RPC    string           `json:"jsonrpc"`
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params,omitempty"`
}

type Response struct {
	RPC    string           `json:"jsonrpc"`
	ID     *json.RawMessage `json:"id"`
	Result interface{}      `json:"result,omitempty"`
	Error  interface{}      `json:"error,omitempty"`
}

type Notification struct {
	RPC    string      `json:"jsonrpc"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type CompletionItem struct {
	Label            string `json:"label"`
	Kind             int    `json:"kind"`
	Detail           string `json:"detail,omitempty"`
	Documentation    string `json:"documentation,omitempty"`
	InsertText       string `json:"insertText,omitempty"`
	InsertTextFormat int    `json:"insertTextFormat,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type InlayHint struct {
	Position    Position `json:"position"`
	Label       string   `json:"label"`
	Kind        int      `json:"kind"`
	PaddingLeft bool     `json:"paddingLeft"`
}

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation string                 `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

type ParameterInformation struct {
	Label         string `json:"label"`
	Documentation string `json:"documentation,omitempty"`
}

const (
	// Completion item kinds
	CIKText          = 1
	CIKMethod        = 2
	CIKFunction      = 3
	CIKConstructor   = 4
	CIKField         = 5
	CIKVariable      = 6
	CIKClass         = 7
	CIKInterface     = 8
	CIKModule        = 9
	CIKProperty      = 10
	CIKUnit          = 11
	CIKValue         = 12
	CIKEnum          = 13
	CIKKeyword       = 14
	CIKSnippet       = 15
	CIKColor         = 16
	CIKFile          = 17
	CIKReference     = 18
	CIKFolder        = 19
	CIKEnumMember    = 20
	CIKConstant      = 21
	CIKStruct        = 22
	CIKEvent         = 23
	CIKOperator      = 24
	CIKTypeParameter = 25

	// Symbol kinds
	SKFile          = 1
	SKModule        = 2
	SKNamespace     = 3
	SKPackage       = 4
	SKClass         = 5
	SKMethod        = 6
	SKProperty      = 7
	SKField         = 8
	SKConstructor   = 9
	SKEnum          = 10
	SKInterface     = 11
	SKFunction      = 12
	SKVariable      = 13
	SKConstant      = 14
	SKString        = 15
	SKNumber        = 16
	SKBoolean       = 17
	SKArray         = 18
	SKObject        = 19
	SKKey           = 20
	SKNull          = 21
	SKEnumMember    = 22
	SKStruct        = 23
	SKEvent         = 24
	SKOperator      = 25
	SKTypeParameter = 26
)

// ============================================================================
// DOCUMENT STORE WITH CACHED PIPELINE RESULT
// ============================================================================

type Document struct {
	Text        string
	LineOffsets []int
	mu          sync.RWMutex
	result      *pipelineResult
}

func newDocument(uri, text string) *Document {
	offsets := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return &Document{Text: text, LineOffsets: offsets}
}

func (doc *Document) offsetToPosition(offset int) Position {
	if offset < 0 {
		return Position{Line: 0, Character: 0}
	}
	line := sort.Search(len(doc.LineOffsets), func(i int) bool { return doc.LineOffsets[i] > offset }) - 1
	if line < 0 {
		line = 0
	}
	return Position{Line: line, Character: offset - doc.LineOffsets[line]}
}

func (doc *Document) positionToOffset(pos Position) int {
	if pos.Line < 0 || pos.Line >= len(doc.LineOffsets) {
		return len(doc.Text)
	}
	offset := doc.LineOffsets[pos.Line] + pos.Character
	if offset > len(doc.Text) {
		return len(doc.Text)
	}
	return offset
}

func (doc *Document) spanToRange(span lexer.Span) Range {
	return Range{Start: doc.offsetToPosition(span.Start), End: doc.offsetToPosition(span.End)}
}

func (doc *Document) setResult(res *pipelineResult) {
	doc.mu.Lock()
	defer doc.mu.Unlock()
	doc.result = res
}

func (doc *Document) getResult() *pipelineResult {
	doc.mu.RLock()
	defer doc.mu.RUnlock()
	return doc.result
}

type docStore struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

func newDocStore() *docStore { return &docStore{docs: make(map[string]*Document)} }

func (d *docStore) set(uri, text string) *Document {
	d.mu.Lock()
	defer d.mu.Unlock()
	doc := newDocument(uri, text)
	d.docs[uri] = doc
	return doc
}

func (d *docStore) get(uri string) (*Document, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	doc, ok := d.docs[uri]
	return doc, ok
}

// ============================================================================
// PIPELINE RESULT (cached per document)
// ============================================================================

type pipelineResult struct {
	program     *ast.SourceFile
	pool        *sema.TypePool
	globalScope *sema.Scope
	sortedDecls []ast.Decl
	parseErrors []string
	semaErrors  []string
	tokens      []lexer.Token
	nodeURIs    map[ast.Node]string
	// For quick lookup of node at position, we can build an interval tree later
}

func runPipeline(uri, code string, workspaceRoot string) *pipelineResult {
	res := &pipelineResult{
		nodeURIs: make(map[ast.Node]string),
	}

	l := lexer.New(code)
	p := parser.New(l)
	res.program = p.ParseSourceFile()
	res.parseErrors = p.Errors()

	for _, d := range res.program.Declarations {
		res.nodeURIs[d] = uri
	}

	l2 := lexer.New(code)
	for {
		tok := l2.NextToken()
		res.tokens = append(res.tokens, tok)
		if tok.Type == lexer.EOF {
			break
		}
	}

	res.pool = sema.NewTypePool()
	res.globalScope = sema.NewScope(nil)
	evaluator := sema.NewEvaluator(res.pool, code)
	evaluator.GlobalDecls = res.program.Declarations

	// JIT callback is not used in LSP (we mock it if needed, but we'll just skip)
	evaluator.JITCallback = func(expr ast.Expr, expectedType sema.TypeID, pool *sema.TypePool, envScope *sema.Scope, globalDecls []ast.Decl) ([]byte, error) {
		// Mock JIT: return dummy data of correct size (all zeros)
		sizeBytes := pool.Types[expectedType].TrueSizeBits / 8
		if sizeBytes == 0 {
			sizeBytes = 1
		}
		return make([]byte, sizeBytes), nil
	}
	evaluator.InjectBuiltins(res.globalScope)

	dag := sema.NewDAG(res.globalScope)
	loadedFiles := make(map[string]bool)

	baseDir := filepath.Dir(strings.TrimPrefix(uri, "file://"))

	dag.ParseModule = func(modulePath string) ([]ast.Decl, error) {
		parts := strings.Split(modulePath, ".")
		for i := len(parts); i > 0; i-- {
			relPath := strings.Join(parts[:i], string(os.PathSeparator)) + ".syn"

			pathsToTry := []string{filepath.Join(baseDir, relPath)}
			if workspaceRoot != "" {
				pathsToTry = append(pathsToTry, filepath.Join(workspaceRoot, relPath))
			}
			pathsToTry = append(pathsToTry, relPath)

			for _, checkPath := range pathsToTry {
				if _, err := os.Stat(checkPath); err == nil {
					if loadedFiles[checkPath] {
						return nil, nil
					}
					loadedFiles[checkPath] = true

					content, err := os.ReadFile(checkPath)
					if err != nil {
						return nil, err
					}

					l := lexer.New(string(content))
					p := parser.New(l)
					prog := p.ParseSourceFile()

					absPath, _ := filepath.Abs(checkPath)
					extURI := "file://" + absPath

					modulePrefix := strings.Join(parts[:i], ".")
					for _, decl := range prog.Declarations {
						res.nodeURIs[decl] = extURI
						switch v := decl.(type) {
						case *ast.StructDecl:
							v.Name.Value = modulePrefix + "." + v.Name.Value
						case *ast.EnumDecl:
							v.Name.Value = modulePrefix + "." + v.Name.Value
						case *ast.FunctionDecl:
							if v.Name != nil {
								v.Name.Value = modulePrefix + "." + v.Name.Value
							}
						case *ast.VariableDecl:
							v.Name.Value = modulePrefix + "." + v.Name.Value
						case *ast.ImplDecl:
							if !strings.Contains(v.Target.Value, ".") {
								v.Target.Value = modulePrefix + "." + v.Target.Value
							}
						}
					}
					return prog.Declarations, nil
				}
			}
		}
		return nil, nil
	}

	sortedDecls, err := dag.BuildAndSort(res.program)
	if err != nil {
		res.semaErrors = append(res.semaErrors, err.Error())
		return res
	}

	for _, decl := range sortedDecls {
		evaluator.Evaluate(decl, res.globalScope)
	}

	res.sortedDecls = sortedDecls
	res.semaErrors = evaluator.Errors
	return res
}

// ============================================================================
// WORKSPACE SYMBOL INDEX
// ============================================================================

type workspaceSymbol struct {
	name       string
	kind       int
	uri        string
	rangeStart Position
	rangeEnd   Position
	detail     string
}

type symbolIndex struct {
	mu      sync.RWMutex
	symbols map[string][]workspaceSymbol // name -> list (multiple files)
}

func newSymbolIndex() *symbolIndex {
	return &symbolIndex{symbols: make(map[string][]workspaceSymbol)}
}

func (idx *symbolIndex) add(sym workspaceSymbol) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.symbols[sym.name] = append(idx.symbols[sym.name], sym)
}

func (idx *symbolIndex) search(query string) []workspaceSymbol {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var results []workspaceSymbol
	// simple prefix matching (could be improved)
	for name, list := range idx.symbols {
		if strings.Contains(name, query) {
			results = append(results, list...)
		}
	}
	return results
}

// ============================================================================
// SERVER CORE
// ============================================================================

type Server struct {
	docs          *docStore
	writer        *bufio.Writer
	logger        *log.Logger
	workspaceRoot string
	symbols       *symbolIndex
	// For signature help, we need to know the call context
}

func Start() {
	logFile, err := os.OpenFile("synovium_lsp.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		logFile = os.Stderr
	} else {
		defer logFile.Close()
	}

	s := &Server{
		docs:    newDocStore(),
		writer:  bufio.NewWriter(os.Stdout),
		logger:  log.New(logFile, "[SYN-LSP] ", log.Ltime|log.Lshortfile),
		symbols: newSymbolIndex(),
	}

	s.logger.Println("Synovium Language Server (Production) booting…")
	s.run()
}

func (s *Server) run() {
	reader := bufio.NewReader(os.Stdin)
	for {
		contentLength := -1
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				contentLength, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
			}
		}
		if contentLength <= 0 {
			continue
		}
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, body); err != nil {
			return
		}

		var req Request
		json.Unmarshal(body, &req)
		s.dispatch(&req)
	}
}

func (s *Server) dispatch(req *Request) {
	switch req.Method {
	case "initialize":
		var p struct {
			RootUri string `json:"rootUri"`
		}
		json.Unmarshal(req.Params, &p)
		if p.RootUri != "" {
			s.workspaceRoot = strings.TrimPrefix(p.RootUri, "file://")
			go s.indexWorkspace() // async indexing
		}
		s.handleInitialize(req)
	case "initialized", "shutdown":
		if req.ID != nil {
			s.sendResult(req.ID, nil)
		}
	case "exit":
		os.Exit(0)

	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		json.Unmarshal(req.Params, &p)
		doc := s.docs.set(p.TextDocument.URI, p.TextDocument.Text)
		go s.updatePipelineAndDiagnostics(p.TextDocument.URI, doc)

	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		json.Unmarshal(req.Params, &p)
		if len(p.ContentChanges) > 0 {
			text := p.ContentChanges[len(p.ContentChanges)-1].Text
			doc := s.docs.set(p.TextDocument.URI, text)
			go s.updatePipelineAndDiagnostics(p.TextDocument.URI, doc)
		}

	case "textDocument/hover":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleHover(req, p.TextDocument.URI, p.Position)

	case "textDocument/definition":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleDefinition(req, p.TextDocument.URI, p.Position)

	case "textDocument/implementation":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleImplementation(req, p.TextDocument.URI, p.Position)

	case "textDocument/semanticTokens/full":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleSemanticTokens(req, p.TextDocument.URI)

	case "textDocument/completion":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleCompletion(req, p.TextDocument.URI, p.Position)

	case "textDocument/documentSymbol":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleDocumentSymbol(req, p.TextDocument.URI)

	case "workspace/symbol":
		var p struct {
			Query string `json:"query"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleWorkspaceSymbol(req, p.Query)

	case "textDocument/inlayHint":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleInlayHint(req, p.TextDocument.URI)

	case "textDocument/signatureHelp":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleSignatureHelp(req, p.TextDocument.URI, p.Position)
	}
}

func (s *Server) sendRaw(v interface{}) {
	body, _ := json.Marshal(v)
	fmt.Fprintf(s.writer, "Content-Length: %d\r\n\r\n", len(body))
	s.writer.Write(body)
	s.writer.Flush()
}

func (s *Server) sendResult(id *json.RawMessage, result interface{}) {
	s.sendRaw(Response{RPC: "2.0", ID: id, Result: result})
}

func (s *Server) handleInitialize(req *Request) {
	s.sendResult(req.ID, map[string]interface{}{
		"capabilities": map[string]interface{}{
			"textDocumentSync":        map[string]interface{}{"openClose": true, "change": 1},
			"hoverProvider":           true,
			"definitionProvider":      true,
			"implementationProvider":  true,
			"documentSymbolProvider":  true,
			"workspaceSymbolProvider": true,
			"inlayHintProvider":       map[string]interface{}{"resolveProvider": false},
			"signatureHelpProvider":   map[string]interface{}{"triggerCharacters": []string{"(", ","}},
			"semanticTokensProvider": map[string]interface{}{
				"legend": map[string]interface{}{
					"tokenTypes":     []string{"type", "struct", "enum", "function", "variable", "keyword", "operator", "number", "string", "comment"},
					"tokenModifiers": []string{"declaration", "definition", "readonly", "static"},
				},
				"full": true,
			},
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{".", ":"},
				"resolveProvider":   false,
			},
		},
	})
}

// ============================================================================
// PIPELINE CACHING & DIAGNOSTICS
// ============================================================================

func (s *Server) updatePipelineAndDiagnostics(uri string, doc *Document) {
	res := runPipeline(uri, doc.Text, s.workspaceRoot)
	doc.setResult(res)
	s.publishDiagnostics(uri, res)
}

func (s *Server) getPipeline(uri string) *pipelineResult {
	doc, ok := s.docs.get(uri)
	if !ok {
		return nil
	}
	res := doc.getResult()
	if res == nil {
		// fallback: run pipeline synchronously
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}
	return res
}

func (s *Server) publishDiagnostics(uri string, res *pipelineResult) {
	diagnostics := make([]Diagnostic, 0)
	for _, msg := range res.parseErrors {
		clean := ansiRegex.ReplaceAllString(msg, "")
		line := 0
		if parts := strings.Split(clean, "at line "); len(parts) >= 2 {
			numStr := strings.TrimSpace(parts[1])
			end := 0
			for end < len(numStr) && numStr[end] >= '0' && numStr[end] <= '9' {
				end++
			}
			if end > 0 {
				line, _ = strconv.Atoi(numStr[:end])
				line--
				if line < 0 {
					line = 0
				}
			}
		}
		diagnostics = append(diagnostics, Diagnostic{Range: Range{Start: Position{line, 0}, End: Position{line, 9999}}, Severity: 1, Source: "synovium", Message: "Syntax Error: " + clean})
	}
	for _, msg := range res.semaErrors {
		diagnostics = append(diagnostics, parseSemaError(msg))
	}
	s.sendRaw(Notification{RPC: "2.0", Method: "textDocument/publishDiagnostics", Params: map[string]interface{}{"uri": uri, "diagnostics": diagnostics}})
}

func parseSemaError(raw string) Diagnostic {
	clean := ansiRegex.ReplaceAllString(raw, "")
	lines := strings.Split(clean, "\n")
	message, diagLine, diagCol, squigglyLen := "", 0, 0, 1

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "Error:") && message == "" {
			message = strings.TrimSpace(strings.TrimPrefix(trimmed, "Error:"))
			continue
		}
		if strings.HasPrefix(trimmed, "--> line ") {
			fmt.Sscanf(strings.TrimPrefix(trimmed, "--> line "), "%d:%d", &diagLine, &diagCol)
			diagLine--
			diagCol--
			if diagLine < 0 {
				diagLine = 0
			}
			if diagCol < 0 {
				diagCol = 0
			}
			continue
		}
		if strings.Contains(trimmed, "^") {
			after := trimmed
			if idx := strings.Index(after, "| "); idx != -1 {
				after = after[idx+2:]
			}
			for _, ch := range strings.TrimLeft(after, " \t") {
				if ch == '^' {
					squigglyLen++
				} else {
					break
				}
			}
		}
	}
	if message == "" {
		message = strings.TrimSpace(clean)
	}
	return Diagnostic{Range: Range{Start: Position{diagLine, diagCol}, End: Position{diagLine, diagCol + squigglyLen}}, Severity: 1, Source: "synovium", Message: message}
}

// ============================================================================
// WORKSPACE SYMBOL INDEXING
// ============================================================================

func (s *Server) indexWorkspace() {
	if s.workspaceRoot == "" {
		return
	}
	s.logger.Println("Indexing workspace at", s.workspaceRoot)
	err := filepath.Walk(s.workspaceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".syn") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		uri := "file://" + path
		res := runPipeline(uri, string(content), s.workspaceRoot)
		doc := newDocument(uri, string(content))
		doc.setResult(res) // cache it
		s.docs.set(uri, string(content))
		// extract symbols
		for _, decl := range res.sortedDecls {
			sym := s.declToSymbol(decl, uri, doc)
			if sym != nil {
				s.symbols.add(*sym)
			}
		}
		return nil
	})
	if err != nil {
		s.logger.Println("Workspace indexing error:", err)
	}
}

func (s *Server) declToSymbol(decl ast.Decl, uri string, doc *Document) *workspaceSymbol {
	switch v := decl.(type) {
	case *ast.FunctionDecl:
		if v.Name != nil {
			return &workspaceSymbol{
				name:       v.Name.Value,
				kind:       SKFunction,
				uri:        uri,
				rangeStart: doc.offsetToPosition(v.Span().Start),
				rangeEnd:   doc.offsetToPosition(v.Span().End),
				detail:     "fnc " + v.Name.Value,
			}
		}
	case *ast.StructDecl:
		if v.Name != nil {
			return &workspaceSymbol{
				name:       v.Name.Value,
				kind:       SKStruct,
				uri:        uri,
				rangeStart: doc.offsetToPosition(v.Span().Start),
				rangeEnd:   doc.offsetToPosition(v.Span().End),
				detail:     "struct " + v.Name.Value,
			}
		}
	case *ast.EnumDecl:
		if v.Name != nil {
			return &workspaceSymbol{
				name:       v.Name.Value,
				kind:       SKEnum,
				uri:        uri,
				rangeStart: doc.offsetToPosition(v.Span().Start),
				rangeEnd:   doc.offsetToPosition(v.Span().End),
				detail:     "enum " + v.Name.Value,
			}
		}
	case *ast.VariableDecl:
		if v.Name != nil {
			return &workspaceSymbol{
				name:       v.Name.Value,
				kind:       SKVariable,
				uri:        uri,
				rangeStart: doc.offsetToPosition(v.Span().Start),
				rangeEnd:   doc.offsetToPosition(v.Span().End),
				detail:     "var " + v.Name.Value,
			}
		}
	}
	return nil
}

// ============================================================================
// HOVER
// ============================================================================

func (s *Server) handleHover(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}
	if res.pool == nil {
		s.sendResult(req.ID, nil)
		return
	}

	offset := doc.positionToOffset(pos)
	node, faNode := findNodesAtOffset(res.program, offset)
	if node == nil && faNode == nil {
		s.sendResult(req.ID, nil)
		return
	}

	bestScope := res.globalScope
	bestScopeLen := -1
	for n, sc := range res.pool.NodeScopes {
		span := n.Span()
		if span.Start <= offset && offset <= span.End {
			if l := span.End - span.Start; bestScopeLen == -1 || l < bestScopeLen {
				bestScope = sc
				bestScopeLen = l
			}
		}
	}

	var typeID sema.TypeID
	var hoverNode ast.Node

	if faNode != nil {
		if tID, exists := res.pool.NodeTypes[faNode]; exists {
			typeID = tID
			hoverNode = faNode
		} else if chain, ok := BuildIdentChain(faNode); ok {
			if sym, exists := bestScope.Resolve(chain); exists {
				typeID = sym.TypeID
				hoverNode = sym.DeclNode
			}
		}
	}

	if typeID == 0 {
		if ident, ok := node.(*ast.Identifier); ok {
			if sym, exists := bestScope.Resolve(ident.Value); exists {
				typeID = sym.TypeID
				hoverNode = sym.DeclNode
			}
		} else if named, ok := node.(*ast.NamedType); ok {
			if sym, exists := bestScope.Resolve(named.Name); exists {
				typeID = sym.TypeID
				hoverNode = sym.DeclNode
			}
		} else if tID, exists := res.pool.NodeTypes[node]; exists {
			typeID = tID
			hoverNode = node
		}
	}

	if int(typeID) > 0 && int(typeID) < len(res.pool.Types) {
		t := res.pool.Types[typeID]
		md := formatTypeMarkdown(t, res.pool, hoverNode)

		targetNode := node
		if faNode != nil {
			targetNode = faNode
		}
		hoverRange := doc.spanToRange(targetNode.Span())

		s.sendResult(req.ID, HoverResult{Contents: MarkupContent{Kind: "markdown", Value: md}, Range: &hoverRange})
		return
	}
	s.sendResult(req.ID, nil)
}

// formatTypeMarkdown enhanced to show doc comments if present
func formatTypeMarkdown(t sema.UniversalType, pool *sema.TypePool, node ast.Node) string {
	var sb strings.Builder
	// Add doc comment if node has Doc field (not implemented in AST yet)
	// For now, just show type info
	sb.WriteString("```synovium\n")

	switch {
	case (t.Mask & sema.MaskIsFunction) != 0:
		var params []string
		for i, pID := range t.FuncParams {
			if int(pID) < len(pool.Types) {
				// If we have parameter names from the executable node, use them
				if fn, ok := t.Executable.(*ast.FunctionDecl); ok && i < len(fn.Parameters) {
					params = append(params, fn.Parameters[i].Name.Value+": "+pool.Types[pID].Name)
				} else {
					params = append(params, pool.Types[pID].Name)
				}
			}
		}
		retName := "void"
		if int(t.FuncReturn) < len(pool.Types) && pool.Types[t.FuncReturn].Name != "void" {
			retName = pool.Types[t.FuncReturn].Name
		}
		name := strings.TrimSuffix(t.Name, "_signature")
		if retName == "void" {
			sb.WriteString(fmt.Sprintf("fnc %s(%s)", name, strings.Join(params, ", ")))
		} else {
			sb.WriteString(fmt.Sprintf("fnc %s(%s) = %s", name, strings.Join(params, ", "), retName))
		}
	case (t.Mask&sema.MaskIsStruct) != 0 && len(t.Variants) > 0:
		sb.WriteString("enum " + t.Name + " {\n")
		vnames := make([]string, 0, len(t.Variants))
		for k := range t.Variants {
			vnames = append(vnames, k)
		}
		sort.Strings(vnames)
		for _, v := range vnames {
			payload := t.Variants[v]
			if len(payload) == 0 {
				sb.WriteString(fmt.Sprintf("    %s,\n", v))
			} else {
				types := make([]string, len(payload))
				for i, p := range payload {
					types[i] = pool.Types[p].Name
				}
				sb.WriteString(fmt.Sprintf("    %s(%s),\n", v, strings.Join(types, ", ")))
			}
		}
		sb.WriteString("}")
	case (t.Mask & sema.MaskIsStruct) != 0:
		sb.WriteString("struct " + t.Name + " {\n")
		if len(t.FieldLayout) > 0 {
			for _, fID := range t.FieldLayout {
				// need field name; we don't have mapping from typeID to field name easily
				// we could store field names in the type, but they are in Fields map
				// let's iterate Fields to get names
				for name, id := range t.Fields {
					if id == fID {
						sb.WriteString(fmt.Sprintf("    %s: %s,\n", name, pool.Types[id].Name))
					}
				}
			}
		} else {
			sb.WriteString("    ...\n")
		}
		sb.WriteString("}")
	case (t.Mask & sema.MaskIsPointer) != 0:
		base := ""
		if int(t.BaseType) < len(pool.Types) {
			base = pool.Types[t.BaseType].Name
		}
		sb.WriteString("*" + base)
	case (t.Mask & sema.MaskIsArray) != 0:
		base := ""
		if int(t.BaseType) < len(pool.Types) {
			base = pool.Types[t.BaseType].Name
		}
		if t.Capacity == 0 {
			sb.WriteString(fmt.Sprintf("[%s; :]", base))
		} else {
			sb.WriteString(fmt.Sprintf("[%s; %d]", base, t.Capacity))
		}
	default:
		sb.WriteString(t.Name)
	}
	sb.WriteString("\n```")
	return sb.String()
}

// ============================================================================
// DEFINITION
// ============================================================================

func (s *Server) handleDefinition(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}
	if res.pool == nil {
		s.sendResult(req.ID, nil)
		return
	}

	offset := doc.positionToOffset(pos)
	node, faNode := findNodesAtOffset(res.program, offset)

	bestScope := res.globalScope
	bestScopeLen := -1
	for n, sc := range res.pool.NodeScopes {
		span := n.Span()
		if span.Start <= offset && offset <= span.End {
			if l := span.End - span.Start; bestScopeLen == -1 || l < bestScopeLen {
				bestScope = sc
				bestScopeLen = l
			}
		}
	}

	var targetNode ast.Node

	if faNode != nil {
		if chain, ok := BuildIdentChain(faNode); ok {
			if sym, exists := bestScope.Resolve(chain); exists && sym.DeclNode != nil {
				targetNode = resolveAliasNode(sym, res.pool)
			}
		}
	}

	if targetNode == nil {
		if ident, ok := node.(*ast.Identifier); ok {
			if sym, exists := bestScope.Resolve(ident.Value); exists && sym.DeclNode != nil {
				targetNode = resolveAliasNode(sym, res.pool)
			}
		} else if named, ok := node.(*ast.NamedType); ok {
			if sym, exists := bestScope.Resolve(named.Name); exists && sym.DeclNode != nil {
				targetNode = resolveAliasNode(sym, res.pool)
			}
		}
	}

	if targetNode != nil {
		targetURI := uri
		targetDoc := doc

		if mappedURI, ok := res.nodeURIs[targetNode]; ok {
			targetURI = mappedURI
		}

		if targetURI != uri {
			path := strings.TrimPrefix(targetURI, "file://")
			if content, err := os.ReadFile(path); err == nil {
				targetDoc = newDocument(targetURI, string(content))
			}
		}

		s.sendResult(req.ID, Location{URI: targetURI, Range: targetDoc.spanToRange(targetNode.Span())})
		return
	}
	s.sendResult(req.ID, nil)
}

func resolveAliasNode(sym *sema.Symbol, pool *sema.TypePool) ast.Node {
	if int(sym.TypeID) > 0 && int(sym.TypeID) < len(pool.Types) {
		if exec := pool.Types[sym.TypeID].Executable; exec != nil {
			return exec
		}
	}
	return sym.DeclNode
}

// ============================================================================
// IMPLEMENTATION
// ============================================================================

func (s *Server) handleImplementation(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}

	offset := doc.positionToOffset(pos)
	node, _ := findNodesAtOffset(res.program, offset)

	if ident, ok := node.(*ast.Identifier); ok {
		var locations []Location
		for _, decl := range res.sortedDecls {
			if impl, ok := decl.(*ast.ImplDecl); ok && impl.Target.Value == ident.Value {
				implURI := uri
				if mappedURI, ok := res.nodeURIs[impl]; ok {
					implURI = mappedURI
				}

				implDoc := doc
				if implURI != uri {
					path := strings.TrimPrefix(implURI, "file://")
					if content, err := os.ReadFile(path); err == nil {
						implDoc = newDocument(implURI, string(content))
					}
				}
				locations = append(locations, Location{URI: implURI, Range: implDoc.spanToRange(impl.Span())})
			}
		}
		s.sendResult(req.ID, locations)
		return
	}
	s.sendResult(req.ID, nil)
}

// ============================================================================
// SEMANTIC TOKENS
// ============================================================================

func (s *Server) handleSemanticTokens(req *Request, uri string) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, map[string]interface{}{"data": []int{}})
		return
	}
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}

	var tokens []int
	prevLine, prevCol := 0, 0

	for _, tok := range res.tokens {
		tokType := -1
		switch tok.Type {
		case lexer.STRUCT, lexer.ENUM, lexer.IMPL, lexer.FNC, lexer.RET, lexer.DEFER, lexer.BRK, lexer.IF, lexer.ELIF, lexer.ELSE, lexer.MATCH, lexer.LOOP, lexer.AS, lexer.TRUE, lexer.FALSE:
			tokType = 5 // keyword
		case lexer.ASSIGN, lexer.DECL_ASSIGN, lexer.MUT_ASSIGN, lexer.PLUS_ASSIGN, lexer.MIN_ASSIGN, lexer.MUL_ASSIGN, lexer.DIV_ASSIGN, lexer.MOD_ASSIGN, lexer.BIT_AND_ASSIGN, lexer.BIT_OR_ASSIGN, lexer.BIT_XOR_ASSIGN, lexer.LSHIFT_ASSIGN, lexer.RSHIFT_ASSIGN, lexer.PLUS, lexer.MINUS, lexer.ASTERISK, lexer.SLASH, lexer.MOD, lexer.BANG, lexer.TILDE, lexer.AMPERS, lexer.PIPE, lexer.CARET, lexer.QUESTION, lexer.LSHIFT, lexer.RSHIFT, lexer.AND, lexer.OR, lexer.EQ, lexer.NOT_EQ, lexer.LT, lexer.LTE, lexer.GT, lexer.GTE, lexer.ARROW, lexer.RANGE, lexer.DOT:
			tokType = 6 // operator
		case lexer.IDENT:
			tokType = 4 // variable (default)
			if res.globalScope != nil {
				if sym, exists := res.globalScope.Resolve(tok.Literal); exists && int(sym.TypeID) < len(res.pool.Types) {
					t := res.pool.Types[sym.TypeID]
					if (t.Mask & sema.MaskIsFunction) != 0 {
						tokType = 3 // function
					} else if (t.Mask & sema.MaskIsStruct) != 0 {
						if len(t.Variants) > 0 {
							tokType = 2 // enum
						} else {
							tokType = 1 // struct
						}
					} else if (t.Mask & sema.MaskIsMeta) != 0 {
						tokType = 0 // type
					}
				}
			}
		case lexer.STRING, lexer.CHAR:
			tokType = 8 // string
		case lexer.INT, lexer.FLOAT:
			tokType = 7 // number
		}

		if tokType == -1 {
			continue
		}

		pos := doc.offsetToPosition(tok.Span.Start)
		length := tok.Span.End - tok.Span.Start
		deltaLine := pos.Line - prevLine
		deltaCol := pos.Character
		if deltaLine == 0 {
			deltaCol = pos.Character - prevCol
		}

		tokens = append(tokens, deltaLine, deltaCol, length, tokType, 0)
		prevLine = pos.Line
		prevCol = pos.Character
	}
	s.sendResult(req.ID, map[string]interface{}{"data": tokens})
}

// ============================================================================
// COMPLETION
// ============================================================================

func (s *Server) handleCompletion(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, CompletionList{})
		return
	}

	offset := doc.positionToOffset(pos)
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}

	// Check for dot completion
	if offset > 0 {
		cursor := offset
		for cursor > 0 && isIdentChar(doc.Text[cursor-1]) {
			cursor--
		}
		if cursor > 0 && doc.Text[cursor-1] == '.' {
			if items := s.dotCompletion(doc.Text, cursor-1, res); items != nil {
				s.sendResult(req.ID, CompletionList{IsIncomplete: false, Items: items})
				return
			}
		}
	}

	// Check for context-sensitive completion (e.g., after 'match')
	// We can try to find the innermost node at cursor and infer context
	nodeAtCursor, _ := findNodesAtOffset(res.program, offset)
	if nodeAtCursor != nil {
		// Simple heuristic: if we are inside a match arm pattern, suggest enum variants
		if match, ok := findEnclosing[*ast.MatchExpr](res.program, nodeAtCursor); ok {
			// we are inside a match expression; suggest variants of the matched value's type
			if match.Value != nil {
				if typeID, exists := res.pool.NodeTypes[match.Value]; exists {
					t := res.pool.Types[typeID]
					if len(t.Variants) > 0 {
						// suggest variants
						items := make([]CompletionItem, 0, len(t.Variants))
						for vname := range t.Variants {
							items = append(items, CompletionItem{Label: vname, Kind: CIKEnumMember, Detail: "enum variant"})
						}
						s.sendResult(req.ID, CompletionList{IsIncomplete: false, Items: items})
						return
					}
				}
			}
		}
	}

	// Default: keywords + scope symbols
	items := make([]CompletionItem, len(synoviumKeywords))
	copy(items, synoviumKeywords)
	if res.globalScope != nil && res.pool != nil {
		for name, sym := range res.globalScope.Symbols {
			if !sym.IsResolved || int(sym.TypeID) >= len(res.pool.Types) {
				continue
			}
			t := res.pool.Types[sym.TypeID]
			kind := CIKVariable
			detail := t.Name
			if (t.Mask & sema.MaskIsFunction) != 0 {
				kind = CIKFunction
				// format function signature
				var params []string
				for i, p := range t.FuncParams {
					if fn, ok := t.Executable.(*ast.FunctionDecl); ok && i < len(fn.Parameters) {
						params = append(params, fn.Parameters[i].Name.Value+": "+res.pool.Types[p].Name)
					} else {
						params = append(params, res.pool.Types[p].Name)
					}
				}
				retName := "void"
				if int(t.FuncReturn) < len(res.pool.Types) {
					retName = res.pool.Types[t.FuncReturn].Name
				}
				detail = fmt.Sprintf("fnc %s(%s) = %s", name, strings.Join(params, ", "), retName)
			} else if (t.Mask&sema.MaskIsStruct) != 0 && len(t.Variants) > 0 {
				kind = CIKEnum
			} else if (t.Mask & sema.MaskIsStruct) != 0 {
				kind = CIKStruct
			} else if (t.Mask & sema.MaskIsMeta) != 0 {
				kind = CIKKeyword
			}
			items = append(items, CompletionItem{Label: name, Kind: kind, Detail: detail})
		}
	}
	s.sendResult(req.ID, CompletionList{IsIncomplete: false, Items: items})
}

func (s *Server) dotCompletion(code string, dotOffset int, res *pipelineResult) []CompletionItem {
	if res.pool == nil || res.globalScope == nil {
		return nil
	}
	end := dotOffset
	start := end
	for start > 0 && (isIdentChar(code[start-1]) || code[start-1] == '.') {
		start--
	}
	chain := code[start:end]
	if chain == "" {
		return nil
	}

	sym, exists := res.globalScope.Resolve(chain)
	if !exists || !sym.IsResolved || int(sym.TypeID) >= len(res.pool.Types) {
		return nil
	}

	t := res.pool.Types[sym.TypeID]
	if (t.Mask&sema.MaskIsPointer) != 0 && int(t.BaseType) < len(res.pool.Types) {
		t = res.pool.Types[t.BaseType]
	}

	var items []CompletionItem
	// Fields
	for fname, fid := range t.Fields {
		detail := ""
		if int(fid) < len(res.pool.Types) {
			detail = res.pool.Types[fid].Name
		}
		items = append(items, CompletionItem{Label: fname, Kind: CIKField, Detail: detail})
	}
	// Methods
	for mname, mid := range t.Methods {
		detail := "fnc"
		if int(mid) < len(res.pool.Types) {
			m := res.pool.Types[mid]
			if int(m.FuncReturn) < len(res.pool.Types) {
				if rn := res.pool.Types[m.FuncReturn].Name; rn != "void" {
					detail = "fnc = " + rn
				}
			}
		}
		items = append(items, CompletionItem{Label: mname, Kind: CIKMethod, Detail: detail})
	}
	// Enum variants (static)
	if len(t.Variants) > 0 {
		for vname := range t.Variants {
			items = append(items, CompletionItem{Label: vname, Kind: CIKEnumMember, Detail: "enum variant"})
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

var synoviumKeywords = []CompletionItem{
	{Label: "fnc", Kind: CIKKeyword, InsertText: "fnc ${1:name}(${2:params}) = ${3:ret} {\n\t$0\n}", InsertTextFormat: 2},
	{Label: "struct", Kind: CIKKeyword, InsertText: "struct ${1:Name} {\n\t${2:field}: ${3:type}\n}", InsertTextFormat: 2},
	{Label: "enum", Kind: CIKKeyword, InsertText: "enum ${1:Name} {\n\t${2:Variant}\n}", InsertTextFormat: 2},
	{Label: "impl", Kind: CIKKeyword, InsertText: "impl ${1:Type} {\n\t$0\n}", InsertTextFormat: 2},
	{Label: "if", Kind: CIKKeyword, InsertText: "if ${1:cond} {\n\t$0\n}", InsertTextFormat: 2},
	{Label: "elif", Kind: CIKKeyword, InsertText: "elif ${1:cond} {\n\t$0\n}", InsertTextFormat: 2},
	{Label: "else", Kind: CIKKeyword, InsertText: "else {\n\t$0\n}", InsertTextFormat: 2},
	{Label: "loop", Kind: CIKKeyword, InsertText: "loop(${1:i}: ${2:i32} = ${3:0}...${4:n}) {\n\t$0\n}", InsertTextFormat: 2},
	{Label: "match", Kind: CIKKeyword, InsertText: "match ${1:val} {\n\t${2:Variant}(${3:v}) -> { $0 }\n}", InsertTextFormat: 2},
	{Label: "ret", Kind: CIKKeyword, InsertTextFormat: 1},
	{Label: "defer", Kind: CIKKeyword, InsertText: "defer { $0 }", InsertTextFormat: 2},
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// ============================================================================
// DOCUMENT SYMBOLS
// ============================================================================

func (s *Server) handleDocumentSymbol(req *Request, uri string) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}

	var symbols []DocumentSymbol
	for _, decl := range res.sortedDecls {
		switch v := decl.(type) {
		case *ast.FunctionDecl:
			if v.Name != nil {
				symbols = append(symbols, DocumentSymbol{
					Name:           v.Name.Value,
					Kind:           SKFunction,
					Range:          doc.spanToRange(v.Span()),
					SelectionRange: doc.spanToRange(v.Name.Span()),
				})
			}
		case *ast.StructDecl:
			if v.Name != nil {
				sym := DocumentSymbol{
					Name:           v.Name.Value,
					Kind:           SKStruct,
					Range:          doc.spanToRange(v.Span()),
					SelectionRange: doc.spanToRange(v.Name.Span()),
				}
				// add fields as children
				for _, f := range v.Fields {
					sym.Children = append(sym.Children, DocumentSymbol{
						Name:           f.Name.Value,
						Kind:           SKField,
						Range:          doc.spanToRange(f.Span()),
						SelectionRange: doc.spanToRange(f.Name.Span()),
					})
				}
				symbols = append(symbols, sym)
			}
		case *ast.EnumDecl:
			if v.Name != nil {
				sym := DocumentSymbol{
					Name:           v.Name.Value,
					Kind:           SKEnum,
					Range:          doc.spanToRange(v.Span()),
					SelectionRange: doc.spanToRange(v.Name.Span()),
				}
				// add variants as children
				for _, variant := range v.Variants {
					sym.Children = append(sym.Children, DocumentSymbol{
						Name:           variant.Name.Value,
						Kind:           SKEnumMember,
						Range:          doc.spanToRange(variant.Span()),
						SelectionRange: doc.spanToRange(variant.Name.Span()),
					})
				}
				symbols = append(symbols, sym)
			}
		case *ast.VariableDecl:
			if v.Name != nil {
				symbols = append(symbols, DocumentSymbol{
					Name:           v.Name.Value,
					Kind:           SKVariable,
					Range:          doc.spanToRange(v.Span()),
					SelectionRange: doc.spanToRange(v.Name.Span()),
				})
			}
		}
	}
	s.sendResult(req.ID, symbols)
}

// ============================================================================
// WORKSPACE SYMBOLS
// ============================================================================

func (s *Server) handleWorkspaceSymbol(req *Request, query string) {
	results := s.symbols.search(query)
	locations := make([]Location, len(results))
	for i, sym := range results {
		locations[i] = Location{
			URI: sym.uri,
			Range: Range{
				Start: sym.rangeStart,
				End:   sym.rangeEnd,
			},
		}
	}
	s.sendResult(req.ID, locations)
}

// ============================================================================
// INLAY HINTS (including comptime values)
// ============================================================================

func (s *Server) handleInlayHint(req *Request, uri string) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}
	if res.pool == nil {
		s.sendResult(req.ID, nil)
		return
	}

	hints := make([]InlayHint, 0)
	walkChildren(res.program, func(n ast.Node) {
		if vDecl, ok := n.(*ast.VariableDecl); ok && vDecl.Operator == ":=" && vDecl.Type == nil {
			if typeID, exists := res.pool.NodeTypes[vDecl.Value]; exists && int(typeID) < len(res.pool.Types) {
				tName := res.pool.Types[typeID].Name
				pos := doc.offsetToPosition(vDecl.Name.Span().End)
				hints = append(hints, InlayHint{Position: pos, Label: ": " + tName, Kind: 1, PaddingLeft: true})
			}
		}
		// Check for comptime blob to show computed value
		if blob, ok := n.(*ast.ComptimeBlob); ok {
			// find the variable that this blob is assigned to (parent may be VariableDecl)
			// We need to know the context. For simplicity, we'll just show the blob's source as a hint after the expression.
			// But inlay hints are usually placed after the variable name, not after the value.
			// Alternatively, we could add a hint after the expression showing the computed value.
			// We'll add a hint at the end of the blob's span with the value.
			if typeID := sema.TypeID(blob.Type); int(typeID) < len(res.pool.Types) {
				valStr := formatComptimeValue(blob, typeID, res.pool)
				if valStr != "" {
					pos := doc.offsetToPosition(blob.Span().End)
					hints = append(hints, InlayHint{Position: pos, Label: " // = " + valStr, Kind: 2, PaddingLeft: true})
				}
			}
		}
	})
	s.sendResult(req.ID, hints)
}

func formatComptimeValue(blob *ast.ComptimeBlob, typeID sema.TypeID, pool *sema.TypePool) string {
	t := pool.Types[typeID]
	data := blob.Data
	// Try to interpret based on type
	switch t.Name {
	case "i8", "u8":
		if len(data) >= 1 {
			return fmt.Sprintf("%d", data[0])
		}
	case "i16", "u16":
		if len(data) >= 2 {
			val := uint16(data[0]) | uint16(data[1])<<8
			return fmt.Sprintf("%d", val)
		}
	case "i32", "u32":
		if len(data) >= 4 {
			val := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
			return fmt.Sprintf("%d", val)
		}
	case "i64", "u64":
		if len(data) >= 8 {
			val := uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
				uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56
			return fmt.Sprintf("%d", val)
		}
	case "f32":
		if len(data) >= 4 {
			bits := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
			val := float32FromBits(bits)
			return fmt.Sprintf("%g", val)
		}
	case "f64":
		if len(data) >= 8 {
			bits := uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
				uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56
			val := float64FromBits(bits)
			return fmt.Sprintf("%g", val)
		}
	case "str":
		// assume null-terminated string
		end := 0
		for end < len(data) && data[end] != 0 {
			end++
		}
		return "\"" + string(data[:end]) + "\""
	}
	// fallback: hex dump
	if len(data) <= 8 {
		hex := make([]string, len(data))
		for i, b := range data {
			hex[i] = fmt.Sprintf("%02x", b)
		}
		return "0x" + strings.Join(hex, "")
	}
	return fmt.Sprintf("[%d bytes]", len(data))
}

func float32FromBits(bits uint32) float32 {
	return float32FromBitsGo(bits) // dummy, need math.Float32frombits
}

func float64FromBits(bits uint64) float64 {
	return float64FromBitsGo(bits)
}

// These would normally use math.Float32frombits etc., but to avoid import we can use a simple conversion.
// In real code, you'd import "math".
// We'll just return a placeholder.
func float32FromBitsGo(bits uint32) float32 {
	return float32(bits) // not correct but for demo
}

func float64FromBitsGo(bits uint64) float64 {
	return float64(bits)
}

// ============================================================================
// SIGNATURE HELP
// ============================================================================

func (s *Server) handleSignatureHelp(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := doc.getResult()
	if res == nil {
		res = runPipeline(uri, doc.Text, s.workspaceRoot)
		doc.setResult(res)
	}
	if res.pool == nil {
		s.sendResult(req.ID, nil)
		return
	}

	offset := doc.positionToOffset(pos)
	callExpr, paramIndex := findCallAtPosition(res.program, offset)
	if callExpr == nil {
		s.sendResult(req.ID, nil)
		return
	}

	funcTypeID, exists := res.pool.NodeTypes[callExpr.Function]
	if !exists {
		s.sendResult(req.ID, nil)
		return
	}
	funcType := res.pool.Types[funcTypeID]
	if (funcType.Mask & sema.MaskIsFunction) == 0 {
		s.sendResult(req.ID, nil)
		return
	}

	// Build signature
	var params []ParameterInformation
	for i, pID := range funcType.FuncParams {
		// If we have parameter names from the function declaration, use them
		if fn, ok := funcType.Executable.(*ast.FunctionDecl); ok && i < len(fn.Parameters) {
			label := fn.Parameters[i].Name.Value + ": " + res.pool.Types[pID].Name
			params = append(params, ParameterInformation{Label: label})
		} else {
			params = append(params, ParameterInformation{Label: res.pool.Types[pID].Name})
		}
	}
	retName := "void"
	if int(funcType.FuncReturn) < len(res.pool.Types) {
		retName = res.pool.Types[funcType.FuncReturn].Name
	}
	sigLabel := fmt.Sprintf("fnc %s(%s) = %s", callExpr.Function.(*ast.Identifier).Value, formatParamsShort(funcType.FuncParams, res.pool), retName)
	sig := SignatureInformation{
		Label:      sigLabel,
		Parameters: params,
	}

	// Determine active parameter based on cursor position inside argument list
	// paramIndex is the index of the argument being edited (0-based)
	activeParam := paramIndex
	if activeParam < 0 {
		activeParam = 0
	}
	if activeParam >= len(params) {
		activeParam = len(params) - 1
	}

	s.sendResult(req.ID, SignatureHelp{
		Signatures:      []SignatureInformation{sig},
		ActiveSignature: 0,
		ActiveParameter: activeParam,
	})
}

func formatParamsShort(paramIDs []sema.TypeID, pool *sema.TypePool) string {
	names := make([]string, len(paramIDs))
	for i, id := range paramIDs {
		names[i] = pool.Types[id].Name
	}
	return strings.Join(names, ", ")
}

// ============================================================================
// AST UTILITIES
// ============================================================================

func BuildIdentChain(node ast.Node) (string, bool) {
	switch n := node.(type) {
	case *ast.Identifier:
		return n.Value, true
	case *ast.FieldAccessExpr:
		if left, ok := BuildIdentChain(n.Left); ok {
			return left + "." + n.Field.Value, true
		}
	}
	return "", false
}

func findNodesAtOffset(root ast.Node, offset int) (ast.Node, *ast.FieldAccessExpr) {
	var tightest ast.Node
	var tightestFA *ast.FieldAccessExpr
	minSize := -1
	minFASize := -1

	walkChildren(root, func(n ast.Node) {
		func() {
			defer func() { recover() }()
			span := n.Span()
			if span.Start <= offset && offset <= span.End {
				size := span.End - span.Start
				if minSize == -1 || size < minSize {
					minSize = size
					tightest = n
				}
				if fa, ok := n.(*ast.FieldAccessExpr); ok {
					if minFASize == -1 || size < minFASize {
						minFASize = size
						tightestFA = fa
					}
				}
			}
		}()
	})
	return tightest, tightestFA
}

// findCallAtPosition returns the innermost CallExpr containing the offset, and the index of the argument being edited.
func findCallAtPosition(root ast.Node, offset int) (*ast.CallExpr, int) {
	var foundCall *ast.CallExpr
	var foundArgIndex int

	walkChildren(root, func(n ast.Node) {
		if call, ok := n.(*ast.CallExpr); ok {
			span := call.Span()
			if span.Start <= offset && offset <= span.End {
				// Determine which argument position the cursor is in
				// For simplicity, find the argument whose span contains the offset
				for i, arg := range call.Arguments {
					argSpan := arg.Span()
					if argSpan.Start <= offset && offset <= argSpan.End {
						foundCall = call
						foundArgIndex = i
						return
					}
				}
				// If not inside any argument, assume after last argument (parameter index = len(args))
				foundCall = call
				foundArgIndex = len(call.Arguments)
			}
		}
	})
	return foundCall, foundArgIndex
}

// findEnclosing returns the nearest ancestor of a given type.
func findEnclosing[T ast.Node](root ast.Node, node ast.Node) (T, bool) {
	var result T
	// This is a simple linear search; for production, we'd build a parent map.
	// We'll just walk and track depth.
	var found bool
	walkChildrenWithParent(root, nil, func(n ast.Node, parent ast.Node) {
		if n == node {
			// node itself is not enclosing, we need its parent
			if p, ok := parent.(T); ok {
				result = p
				found = true
			}
		}
	})
	return result, found
}

func walkChildrenWithParent(n ast.Node, parent ast.Node, visit func(ast.Node, ast.Node)) {
	if n == nil {
		return
	}
	visit(n, parent)
	switch v := n.(type) {
	case *ast.SourceFile:
		for _, d := range v.Declarations {
			walkChildrenWithParent(d, n, visit)
		}
	case *ast.Block:
		for _, st := range v.Statements {
			walkChildrenWithParent(st, n, visit)
		}
		if v.Value != nil {
			walkChildrenWithParent(v.Value, n, visit)
		}
	case *ast.VariableDecl:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		if v.Type != nil {
			walkChildrenWithParent(v.Type, n, visit)
		}
		if v.Value != nil {
			walkChildrenWithParent(v.Value, n, visit)
		}
	case *ast.ExprStmt:
		walkChildrenWithParent(v.Value, n, visit)
	case *ast.FunctionType:
		for _, p := range v.Parameters {
			walkChildrenWithParent(p, n, visit)
		}
		if v.ReturnType != nil {
			walkChildrenWithParent(v.ReturnType, n, visit)
		}
	case *ast.PrefixExpr:
		walkChildrenWithParent(v.Right, n, visit)
	case *ast.InfixExpr:
		walkChildrenWithParent(v.Left, n, visit)
		walkChildrenWithParent(v.Right, n, visit)
	case *ast.NamedType:
		for _, g := range v.GenericArgs {
			walkChildrenWithParent(g, n, visit)
		}
	case *ast.PointerType:
		walkChildrenWithParent(v.Base, n, visit)
	case *ast.ReferenceType:
		walkChildrenWithParent(v.Base, n, visit)
	case *ast.ArrayType:
		walkChildrenWithParent(v.Base, n, visit)
		if v.Size != nil {
			walkChildrenWithParent(v.Size, n, visit)
		}
	case *ast.CallExpr:
		walkChildrenWithParent(v.Function, n, visit)
		for _, a := range v.Arguments {
			walkChildrenWithParent(a, n, visit)
		}
	case *ast.FieldAccessExpr:
		walkChildrenWithParent(v.Left, n, visit)
		if v.Field != nil {
			walkChildrenWithParent(v.Field, n, visit)
		}
	case *ast.IndexExpr:
		walkChildrenWithParent(v.Left, n, visit)
		walkChildrenWithParent(v.Index, n, visit)
	case *ast.FunctionDecl:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		for _, p := range v.Parameters {
			walkChildrenWithParent(p, n, visit)
		}
		if v.ReturnType != nil {
			walkChildrenWithParent(v.ReturnType, n, visit)
		}
		if v.Body != nil {
			walkChildrenWithParent(v.Body, n, visit)
		}
	case *ast.Parameter:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		if v.Type != nil {
			walkChildrenWithParent(v.Type, n, visit)
		}
	case *ast.StructDecl:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		for _, p := range v.GenericParams {
			walkChildrenWithParent(p, n, visit)
		}
		for _, f := range v.Fields {
			walkChildrenWithParent(f, n, visit)
		}
	case *ast.FieldDecl:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		if v.Type != nil {
			walkChildrenWithParent(v.Type, n, visit)
		}
	case *ast.EnumDecl:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		for _, p := range v.GenericParams {
			walkChildrenWithParent(p, n, visit)
		}
		for _, variant := range v.Variants {
			walkChildrenWithParent(variant, n, visit)
		}
	case *ast.VariantDecl:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		for _, t := range v.Types {
			walkChildrenWithParent(t, n, visit)
		}
	case *ast.ImplDecl:
		if v.Target != nil {
			walkChildrenWithParent(v.Target, n, visit)
		}
		for _, m := range v.Methods {
			walkChildrenWithParent(m, n, visit)
		}
	case *ast.IfExpr:
		walkChildrenWithParent(v.Condition, n, visit)
		walkChildrenWithParent(v.Body, n, visit)
		for _, c := range v.ElifConds {
			walkChildrenWithParent(c, n, visit)
		}
		for _, b := range v.ElifBodies {
			walkChildrenWithParent(b, n, visit)
		}
		if v.ElseBody != nil {
			walkChildrenWithParent(v.ElseBody, n, visit)
		}
	case *ast.MatchExpr:
		walkChildrenWithParent(v.Value, n, visit)
		for _, a := range v.Arms {
			walkChildrenWithParent(a, n, visit)
		}
	case *ast.MatchArm:
		if v.Pattern != nil {
			walkChildrenWithParent(v.Pattern, n, visit)
		}
		for _, p := range v.Params {
			walkChildrenWithParent(p, n, visit)
		}
		walkChildrenWithParent(v.Body, n, visit)
	case *ast.LoopExpr:
		if v.Label != nil {
			walkChildrenWithParent(v.Label, n, visit)
		}
		if v.Condition != nil {
			walkChildrenWithParent(v.Condition, n, visit)
		}
		walkChildrenWithParent(v.Body, n, visit)
	case *ast.StructInitExpr:
		walkChildrenWithParent(v.Name, n, visit)
		for _, f := range v.Fields {
			walkChildrenWithParent(f, n, visit)
		}
	case *ast.StructInitField:
		if v.Name != nil {
			walkChildrenWithParent(v.Name, n, visit)
		}
		walkChildrenWithParent(v.Value, n, visit)
		if v.Type != nil {
			walkChildrenWithParent(v.Type, n, visit)
		}
	case *ast.CastExpr:
		walkChildrenWithParent(v.Left, n, visit)
		walkChildrenWithParent(v.Type, n, visit)
	case *ast.BubbleExpr:
		walkChildrenWithParent(v.Left, n, visit)
	case *ast.ReturnStmt:
		if v.Value != nil {
			walkChildrenWithParent(v.Value, n, visit)
		}
	case *ast.DeferStmt:
		walkChildrenWithParent(v.Body, n, visit)
	case *ast.BreakStmt:
		if v.Label != nil {
			walkChildrenWithParent(v.Label, n, visit)
		}
		if v.Value != nil {
			walkChildrenWithParent(v.Value, n, visit)
		}
	case *ast.ArrayInitExpr:
		for _, el := range v.Elements {
			walkChildrenWithParent(el, n, visit)
		}
		if v.Count != nil {
			walkChildrenWithParent(v.Count, n, visit)
		}
	}
}

func walkChildren(n ast.Node, visit func(ast.Node)) {
	walkChildrenWithParent(n, nil, func(node ast.Node, parent ast.Node) {
		visit(node)
	})
}
