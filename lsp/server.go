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
	"synovium/codegen"
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

const (
	CIKMethod     = 2
	CIKFunction   = 3
	CIKField      = 5
	CIKVariable   = 6
	CIKEnum       = 13
	CIKKeyword    = 14
	CIKEnumMember = 20
	CIKStruct     = 22
	SKFunction    = 12
	SKVariable    = 13
	SKStruct      = 23
	SKEnum        = 10
)

// ============================================================================
// DOCUMENT STORE
// ============================================================================

type Document struct {
	Text        string
	LineOffsets []int
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

type docStore struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

func newDocStore() *docStore { return &docStore{docs: make(map[string]*Document)} }

func (d *docStore) set(uri, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.docs[uri] = newDocument(uri, text)
}

func (d *docStore) get(uri string) (*Document, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	doc, ok := d.docs[uri]
	return doc, ok
}

// ============================================================================
// SERVER PIPELINE
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

	evaluator.JITCallback = codegen.RunJIT
	evaluator.InjectBuiltins(res.globalScope)

	dag := sema.NewDAG(res.globalScope)
	loadedFiles := make(map[string]bool)

	// THE FIX: Smart Context-Aware Path Resolution
	baseDir := filepath.Dir(strings.TrimPrefix(uri, "file://"))

	dag.ParseModule = func(modulePath string) ([]ast.Decl, error) {
		parts := strings.Split(modulePath, ".")
		for i := len(parts); i > 0; i-- {
			relPath := strings.Join(parts[:i], string(os.PathSeparator)) + ".syn"

			// Build fallback search paths
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
						res.nodeURIs[decl] = extURI // Map node to exact origin file
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
// SERVER CORE
// ============================================================================

type Server struct {
	docs          *docStore
	writer        *bufio.Writer
	logger        *log.Logger
	workspaceRoot string
}

func Start() {
	logFile, err := os.OpenFile("synovium_lsp.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		logFile = os.Stderr
	} else {
		defer logFile.Close()
	}

	s := &Server{
		docs:   newDocStore(),
		writer: bufio.NewWriter(os.Stdout),
		logger: log.New(logFile, "[SYN-LSP] ", log.Ltime|log.Lshortfile),
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
		s.docs.set(p.TextDocument.URI, p.TextDocument.Text)
		go s.publishDiagnostics(p.TextDocument.URI, p.TextDocument.Text)

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
			s.docs.set(p.TextDocument.URI, text)
			go s.publishDiagnostics(p.TextDocument.URI, text)
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
		s.sendResult(req.ID, []Location{})

	case "textDocument/inlayHint":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleInlayHint(req, p.TextDocument.URI)
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
			"semanticTokensProvider": map[string]interface{}{
				"legend": map[string]interface{}{
					"tokenTypes":     []string{"type", "struct", "enum", "function", "variable", "keyword", "operator", "number", "string"},
					"tokenModifiers": []string{"declaration"},
				},
				"full": true,
			},
			"completionProvider": map[string]interface{}{"triggerCharacters": []string{".", ":"}},
		},
	})
}

// ============================================================================
// DIAGNOSTICS
// ============================================================================

func (s *Server) publishDiagnostics(uri, code string) {
	// THE FIX: explicitly allocate empty slice to prevent Lua 'null' panics
	diagnostics := make([]Diagnostic, 0)
	res := runPipeline(uri, code, s.workspaceRoot)

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
// DEFINITION & HOVER & IMPLEMENTATION
// ============================================================================

// BuildIdentChain extracts full nested chains (math.linal.mat.Matrix)
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

// resolveAliasNode tunnels through types to find the actual Struct/Func declarations
func resolveAliasNode(sym *sema.Symbol, pool *sema.TypePool) ast.Node {
	if int(sym.TypeID) > 0 && int(sym.TypeID) < len(pool.Types) {
		if exec := pool.Types[sym.TypeID].Executable; exec != nil {
			return exec
		}
	}
	return sym.DeclNode
}

func (s *Server) handleDefinition(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := runPipeline(uri, doc.Text, s.workspaceRoot)
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

	// 1. Module Path Resolution (e.g. math.linal.mat.Matrix)
	if faNode != nil {
		if chain, ok := BuildIdentChain(faNode); ok {
			if sym, exists := bestScope.Resolve(chain); exists && sym.DeclNode != nil {
				targetNode = resolveAliasNode(sym, res.pool)
			}
		}
	}

	// 2. Standard Identifier / NamedType Resolution
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

	// 3. Complete the Jump
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

func (s *Server) handleHover(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := runPipeline(uri, doc.Text, s.workspaceRoot)
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

	if faNode != nil {
		if tID, exists := res.pool.NodeTypes[faNode]; exists {
			typeID = tID
		} else if chain, ok := BuildIdentChain(faNode); ok {
			if sym, exists := bestScope.Resolve(chain); exists {
				typeID = sym.TypeID
			}
		}
	}

	if typeID == 0 {
		if ident, ok := node.(*ast.Identifier); ok {
			if sym, exists := bestScope.Resolve(ident.Value); exists {
				typeID = sym.TypeID
			}
		} else if named, ok := node.(*ast.NamedType); ok {
			if sym, exists := bestScope.Resolve(named.Name); exists {
				typeID = sym.TypeID
			}
		} else if tID, exists := res.pool.NodeTypes[node]; exists {
			typeID = tID
		}
	}

	if int(typeID) > 0 && int(typeID) < len(res.pool.Types) {
		t := res.pool.Types[typeID]
		md := formatTypeMarkdown(t, res.pool)

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

func (s *Server) handleImplementation(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := runPipeline(uri, doc.Text, s.workspaceRoot)

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

func formatTypeMarkdown(t sema.UniversalType, pool *sema.TypePool) string {
	var sb strings.Builder
	sb.WriteString("```synovium\n")

	switch {
	case (t.Mask & sema.MaskIsFunction) != 0:
		var params []string
		for _, pID := range t.FuncParams {
			if int(pID) < len(pool.Types) {
				params = append(params, pool.Types[pID].Name)
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
			sb.WriteString(fmt.Sprintf("    %s,\n", v))
		}
		sb.WriteString("}")
	case (t.Mask & sema.MaskIsStruct) != 0:
		sb.WriteString("struct " + t.Name + " {\n...}")
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
		sb.WriteString(fmt.Sprintf("[%s; %d]", base, t.Capacity))
	default:
		sb.WriteString(t.Name)
	}
	sb.WriteString("\n```")
	return sb.String()
}

// ============================================================================
// WORKSPACE & INLAY HINTS
// ============================================================================

func (s *Server) handleDocumentSymbol(req *Request, uri string) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := runPipeline(uri, doc.Text, s.workspaceRoot)

	var symbols []DocumentSymbol
	for _, decl := range res.sortedDecls {
		switch v := decl.(type) {
		case *ast.FunctionDecl:
			if v.Name != nil {
				symbols = append(symbols, DocumentSymbol{Name: v.Name.Value, Kind: SKFunction, Range: doc.spanToRange(v.Span()), SelectionRange: doc.spanToRange(v.Name.Span())})
			}
		case *ast.StructDecl:
			if v.Name != nil {
				symbols = append(symbols, DocumentSymbol{Name: v.Name.Value, Kind: SKStruct, Range: doc.spanToRange(v.Span()), SelectionRange: doc.spanToRange(v.Name.Span())})
			}
		case *ast.EnumDecl:
			if v.Name != nil {
				symbols = append(symbols, DocumentSymbol{Name: v.Name.Value, Kind: SKEnum, Range: doc.spanToRange(v.Span()), SelectionRange: doc.spanToRange(v.Name.Span())})
			}
		case *ast.VariableDecl:
			if v.Name != nil {
				symbols = append(symbols, DocumentSymbol{Name: v.Name.Value, Kind: SKVariable, Range: doc.spanToRange(v.Span()), SelectionRange: doc.spanToRange(v.Name.Span())})
			}
		}
	}
	s.sendResult(req.ID, symbols)
}

func (s *Server) handleInlayHint(req *Request, uri string) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}
	res := runPipeline(uri, doc.Text, s.workspaceRoot)
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
	})
	s.sendResult(req.ID, hints)
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
	res := runPipeline(uri, doc.Text, s.workspaceRoot)

	var tokens []int
	prevLine, prevCol := 0, 0

	for _, tok := range res.tokens {
		tokType := -1
		switch tok.Type {
		case lexer.STRUCT, lexer.ENUM, lexer.IMPL, lexer.FNC, lexer.RET, lexer.DEFER, lexer.BRK, lexer.IF, lexer.ELIF, lexer.ELSE, lexer.MATCH, lexer.LOOP, lexer.AS, lexer.TRUE, lexer.FALSE:
			tokType = 5
		case lexer.ASSIGN, lexer.DECL_ASSIGN, lexer.MUT_ASSIGN, lexer.PLUS_ASSIGN, lexer.MIN_ASSIGN, lexer.MUL_ASSIGN, lexer.DIV_ASSIGN, lexer.MOD_ASSIGN, lexer.BIT_AND_ASSIGN, lexer.BIT_OR_ASSIGN, lexer.BIT_XOR_ASSIGN, lexer.LSHIFT_ASSIGN, lexer.RSHIFT_ASSIGN, lexer.PLUS, lexer.MINUS, lexer.ASTERISK, lexer.SLASH, lexer.MOD, lexer.BANG, lexer.TILDE, lexer.AMPERS, lexer.PIPE, lexer.CARET, lexer.QUESTION, lexer.LSHIFT, lexer.RSHIFT, lexer.AND, lexer.OR, lexer.EQ, lexer.NOT_EQ, lexer.LT, lexer.LTE, lexer.GT, lexer.GTE, lexer.ARROW, lexer.RANGE, lexer.DOT:
			tokType = 6
		case lexer.IDENT:
			tokType = 4
			if res.globalScope != nil {
				if sym, exists := res.globalScope.Resolve(tok.Literal); exists && int(sym.TypeID) < len(res.pool.Types) {
					t := res.pool.Types[sym.TypeID]
					if (t.Mask & sema.MaskIsFunction) != 0 {
						tokType = 3
					}
					if (t.Mask & sema.MaskIsStruct) != 0 {
						tokType = 1
					}
					if (t.Mask & sema.MaskIsMeta) != 0 {
						tokType = 0
					}
				}
			}
		case lexer.STRING, lexer.CHAR:
			tokType = 8
		case lexer.INT, lexer.FLOAT:
			tokType = 7
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
	res := runPipeline(uri, doc.Text, s.workspaceRoot)

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

	items := make([]CompletionItem, len(synoviumKeywords))
	copy(items, synoviumKeywords)
	if res.globalScope != nil && res.pool != nil {
		for name, sym := range res.globalScope.Symbols {
			if !sym.IsResolved || int(sym.TypeID) >= len(res.pool.Types) {
				continue
			}
			t := res.pool.Types[sym.TypeID]
			kind := CIKVariable
			if (t.Mask & sema.MaskIsFunction) != 0 {
				kind = CIKFunction
			}
			if (t.Mask&sema.MaskIsStruct) != 0 && len(t.Variants) > 0 {
				kind = CIKEnum
			}
			if (t.Mask & sema.MaskIsStruct) != 0 {
				kind = CIKStruct
			}
			if (t.Mask & sema.MaskIsMeta) != 0 {
				kind = CIKKeyword
			}
			items = append(items, CompletionItem{Label: name, Kind: kind, Detail: t.Name})
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
	for fname, fid := range t.Fields {
		detail := ""
		if int(fid) < len(res.pool.Types) {
			detail = res.pool.Types[fid].Name
		}
		items = append(items, CompletionItem{Label: fname, Kind: CIKField, Detail: detail})
	}
	for mname, mid := range t.Methods {
		detail := "fnc"
		if int(mid) < len(res.pool.Types) && int(res.pool.Types[mid].FuncReturn) < len(res.pool.Types) {
			if rn := res.pool.Types[res.pool.Types[mid].FuncReturn].Name; rn != "void" {
				detail = "fnc = " + rn
			}
		}
		items = append(items, CompletionItem{Label: mname, Kind: CIKMethod, Detail: detail})
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
// AST TRAVERSER
// ============================================================================

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

func walkChildren(n ast.Node, visit func(ast.Node)) {
	if n == nil {
		return
	}
	visit(n) // Visit self first
	switch v := n.(type) {
	case *ast.SourceFile:
		for _, d := range v.Declarations {
			walkChildren(d, visit)
		}
	case *ast.Block:
		for _, st := range v.Statements {
			walkChildren(st, visit)
		}
		if v.Value != nil {
			walkChildren(v.Value, visit)
		}
	case *ast.VariableDecl:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		if v.Type != nil {
			walkChildren(v.Type, visit)
		}
		if v.Value != nil {
			walkChildren(v.Value, visit)
		}
	case *ast.ExprStmt:
		walkChildren(v.Value, visit)
	case *ast.FunctionType:
		for _, p := range v.Parameters {
			walkChildren(p, visit)
		}
		if v.ReturnType != nil {
			walkChildren(v.ReturnType, visit)
		}
	case *ast.PrefixExpr:
		walkChildren(v.Right, visit)
	case *ast.InfixExpr:
		walkChildren(v.Left, visit)
		walkChildren(v.Right, visit)
	case *ast.NamedType:
		for _, g := range v.GenericArgs {
			walkChildren(g, visit)
		}
	case *ast.PointerType:
		walkChildren(v.Base, visit)
	case *ast.ReferenceType:
		walkChildren(v.Base, visit)
	case *ast.ArrayType:
		walkChildren(v.Base, visit)
		if v.Size != nil {
			walkChildren(v.Size, visit)
		}
	case *ast.CallExpr:
		walkChildren(v.Function, visit)
		for _, a := range v.Arguments {
			walkChildren(a, visit)
		}
	case *ast.FieldAccessExpr:
		walkChildren(v.Left, visit)
		if v.Field != nil {
			walkChildren(v.Field, visit)
		}
	case *ast.IndexExpr:
		walkChildren(v.Left, visit)
		walkChildren(v.Index, visit)
	case *ast.FunctionDecl:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		for _, p := range v.Parameters {
			walkChildren(p, visit)
		}
		if v.ReturnType != nil {
			walkChildren(v.ReturnType, visit)
		}
		if v.Body != nil {
			walkChildren(v.Body, visit)
		}
	case *ast.Parameter:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		if v.Type != nil {
			walkChildren(v.Type, visit)
		}
	case *ast.StructDecl:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		for _, p := range v.GenericParams {
			walkChildren(p, visit)
		}
		for _, f := range v.Fields {
			walkChildren(f, visit)
		}
	case *ast.FieldDecl:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		if v.Type != nil {
			walkChildren(v.Type, visit)
		}
	case *ast.EnumDecl:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		for _, p := range v.GenericParams {
			walkChildren(p, visit)
		}
		for _, variant := range v.Variants {
			walkChildren(variant, visit)
		}
	case *ast.VariantDecl:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		for _, t := range v.Types {
			walkChildren(t, visit)
		}
	case *ast.ImplDecl:
		if v.Target != nil {
			walkChildren(v.Target, visit)
		}
		for _, m := range v.Methods {
			walkChildren(m, visit)
		}
	case *ast.IfExpr:
		walkChildren(v.Condition, visit)
		walkChildren(v.Body, visit)
		for _, c := range v.ElifConds {
			walkChildren(c, visit)
		}
		for _, b := range v.ElifBodies {
			walkChildren(b, visit)
		}
		if v.ElseBody != nil {
			walkChildren(v.ElseBody, visit)
		}
	case *ast.MatchExpr:
		walkChildren(v.Value, visit)
		for _, a := range v.Arms {
			walkChildren(a, visit)
		}
	case *ast.MatchArm:
		if v.Pattern != nil {
			walkChildren(v.Pattern, visit)
		}
		for _, p := range v.Params {
			walkChildren(p, visit)
		}
		walkChildren(v.Body, visit)
	case *ast.LoopExpr:
		if v.Label != nil {
			walkChildren(v.Label, visit)
		}
		if v.Condition != nil {
			walkChildren(v.Condition, visit)
		}
		walkChildren(v.Body, visit)
	case *ast.StructInitExpr:
		walkChildren(v.Name, visit)
		for _, f := range v.Fields {
			walkChildren(f, visit)
		}
	case *ast.StructInitField:
		if v.Name != nil {
			walkChildren(v.Name, visit)
		}
		walkChildren(v.Value, visit)
		if v.Type != nil {
			walkChildren(v.Type, visit)
		}
	case *ast.CastExpr:
		walkChildren(v.Left, visit)
		walkChildren(v.Type, visit)
	case *ast.BubbleExpr:
		walkChildren(v.Left, visit)
	case *ast.ReturnStmt:
		if v.Value != nil {
			walkChildren(v.Value, visit)
		}
	case *ast.DeferStmt:
		walkChildren(v.Body, visit)
	case *ast.BreakStmt:
		if v.Label != nil {
			walkChildren(v.Label, visit)
		}
		if v.Value != nil {
			walkChildren(v.Value, visit)
		}
	case *ast.ArrayInitExpr:
		for _, el := range v.Elements {
			walkChildren(el, visit)
		}
		if v.Count != nil {
			walkChildren(v.Count, visit)
		}
	}
}
