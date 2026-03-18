package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
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

// ============================================================================
// REGEX
// ============================================================================

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ============================================================================
// JSON-RPC TRANSPORT TYPES
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

// ============================================================================
// LSP PROTOCOL TYPES
// ============================================================================

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

const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

const (
	CIKText       = 1
	CIKMethod     = 2
	CIKFunction   = 3
	CIKField      = 5
	CIKVariable   = 6
	CIKKeyword    = 14
	CIKEnum       = 13
	CIKEnumMember = 20
	CIKStruct     = 22
)

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

// ============================================================================
// DOCUMENT STORE (O(log N) Indexed)
// ============================================================================

type Document struct {
	Text        string
	LineOffsets []int // Caches the byte offset of the start of each line
}

type docStore struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

func newDocStore() *docStore {
	return &docStore{docs: make(map[string]*Document)}
}

func (d *docStore) set(uri, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Precompute line offsets for O(log N) positional lookups
	offsets := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}

	d.docs[uri] = &Document{
		Text:        text,
		LineOffsets: offsets,
	}
}

func (d *docStore) get(uri string) (*Document, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	doc, ok := d.docs[uri]
	return doc, ok
}

func (d *docStore) del(uri string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.docs, uri)
}

func (doc *Document) offsetToPosition(offset int) Position {
	if offset < 0 {
		return Position{Line: 0, Character: 0}
	}

	// Binary search for the line
	line := sort.Search(len(doc.LineOffsets), func(i int) bool {
		return doc.LineOffsets[i] > offset
	}) - 1

	if line < 0 {
		line = 0
	}

	char := offset - doc.LineOffsets[line]
	return Position{Line: line, Character: char}
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
	return Range{
		Start: doc.offsetToPosition(span.Start),
		End:   doc.offsetToPosition(span.End),
	}
}

// ============================================================================
// PIPELINE
// ============================================================================

type pipelineResult struct {
	pool        *sema.TypePool
	globalScope *sema.Scope
	sortedDecls []ast.Decl
	parseErrors []string
	semaErrors  []string
	tokens      []lexer.Token // Needed for Semantic Highlighting
}

func runPipeline(code string) *pipelineResult {
	res := &pipelineResult{}

	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseSourceFile()
	res.parseErrors = p.Errors()

	// Re-lex to save tokens for semantic highlighting
	l2 := lexer.New(code)
	for {
		tok := l2.NextToken()
		res.tokens = append(res.tokens, tok)
		if tok.Type == lexer.EOF {
			break
		}
	}

	pool := sema.NewTypePool()
	globalScope := sema.NewScope(nil)
	evaluator := sema.NewEvaluator(pool, code)
	evaluator.GlobalDecls = program.Declarations

	evaluator.JITCallback = func(
		expr ast.Expr, targetType sema.TypeID, pool *sema.TypePool,
		envScope *sema.Scope, globalDecls []ast.Decl,
	) ([]byte, error) {
		size := pool.Types[targetType].TrueSizeBits / 8
		if size == 0 {
			size = 1
		}
		return make([]byte, size), nil
	}

	evaluator.InjectBuiltins(globalScope)

	dag := sema.NewDAG(globalScope)

	loadedFiles := make(map[string]bool)
	dag.ParseModule = func(modulePath string) ([]ast.Decl, error) {
		parts := strings.Split(modulePath, ".")

		for i := len(parts); i > 0; i-- {
			filename := strings.Join(parts[:i], string(os.PathSeparator)) + ".syn"

			if _, err := os.Stat(filename); err == nil {
				if loadedFiles[filename] {
					return nil, nil // Already in the DAG queue!
				}
				loadedFiles[filename] = true

				content, err := os.ReadFile(filename)
				if err != nil {
					return nil, err
				}

				l := lexer.New(string(content))
				p := parser.New(l)
				prog := p.ParseSourceFile()

				if len(p.Errors()) > 0 {
					return nil, fmt.Errorf("parse errors in module '%s'", filename)
				}

				// ACT 2: AST Module Prefix Injection
				modulePrefix := strings.Join(parts[:i], ".")
				for _, decl := range prog.Declarations {
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
		return nil, nil
	}

	sortedDecls, err := dag.BuildAndSort(program)
	if err != nil {
		res.semaErrors = append(res.semaErrors, err.Error())
		res.pool = pool
		res.globalScope = globalScope
		return res
	}

	for _, decl := range sortedDecls {
		evaluator.Evaluate(decl, globalScope)
	}

	res.pool = pool
	res.globalScope = globalScope
	res.sortedDecls = sortedDecls
	res.semaErrors = evaluator.Errors
	return res
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
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_'
}

// ============================================================================
// SERVER
// ============================================================================

type Server struct {
	docs   *docStore
	writer *bufio.Writer
	logger *log.Logger
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

	s.logger.Println("Synovium Language Server booting…")
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
				lenStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
				contentLength, _ = strconv.Atoi(lenStr)
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
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}

		s.logger.Printf("← %s", req.Method)
		s.dispatch(&req)
	}
}

func (s *Server) dispatch(req *Request) {
	switch req.Method {
	case "initialize":
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

	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(req.Params, &p)
		s.docs.del(p.TextDocument.URI)
		go s.sendDiagnostics(p.TextDocument.URI, []Diagnostic{})

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

	default:
		if req.ID != nil {
			s.sendError(req.ID, -32601, "method not found: "+req.Method)
		}
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

func (s *Server) sendError(id *json.RawMessage, code int, msg string) {
	s.sendRaw(map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": map[string]interface{}{"code": code, "message": msg}})
}

func (s *Server) sendDiagnostics(uri string, diagnostics []Diagnostic) {
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	s.sendRaw(Notification{
		RPC:    "2.0",
		Method: "textDocument/publishDiagnostics",
		Params: PublishDiagnosticsParams{URI: uri, Diagnostics: diagnostics},
	})
}

func (s *Server) handleInitialize(req *Request) {
	s.sendResult(req.ID, map[string]interface{}{
		"capabilities": map[string]interface{}{
			"textDocumentSync":   map[string]interface{}{"openClose": true, "change": 1},
			"hoverProvider":      true,
			"definitionProvider": true, // GOTO DEFINITION
			"semanticTokensProvider": map[string]interface{}{ // SEMANTIC TOKENS
				"legend": map[string]interface{}{
					"tokenTypes":     []string{"type", "struct", "enum", "function", "variable", "keyword", "operator", "number", "string"},
					"tokenModifiers": []string{"declaration"},
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
// DIAGNOSTICS & PARSING
// ============================================================================

func (s *Server) publishDiagnostics(uri, code string) {
	//doc, _ := s.docs.get(uri)
	var diagnostics []Diagnostic
	res := runPipeline(code)

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
		diagnostics = append(diagnostics, Diagnostic{
			Range:    Range{Start: Position{line, 0}, End: Position{line, 9999}},
			Severity: SeverityError, Source: "synovium(parse)", Message: "Syntax Error: " + clean,
		})
	}

	for _, msg := range res.semaErrors {
		diagnostics = append(diagnostics, parseSemaError(msg))
	}

	s.sendDiagnostics(uri, diagnostics)
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
			coords := strings.TrimPrefix(trimmed, "--> line ")
			fmt.Sscanf(coords, "%d:%d", &diagLine, &diagCol)
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
			carets := strings.TrimLeft(after, " \t")
			count := 0
			for _, ch := range carets {
				if ch == '^' {
					count++
				} else {
					break
				}
			}
			if count > 0 {
				squigglyLen = count
			}
		}
	}
	if message == "" {
		message = strings.TrimSpace(clean)
	}

	return Diagnostic{
		Range:    Range{Start: Position{diagLine, diagCol}, End: Position{diagLine, diagCol + squigglyLen}},
		Severity: SeverityError, Source: "synovium", Message: message,
	}
}

// ============================================================================
// DEFINITION & HOVER
// ============================================================================

func (s *Server) handleDefinition(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}

	res := runPipeline(doc.Text)
	if res.pool == nil {
		s.sendResult(req.ID, nil)
		return
	}

	offset := doc.positionToOffset(pos)
	node, _ := findNodeAtOffset(res.pool, offset)

	if ident, ok := node.(*ast.Identifier); ok {
		// 1. Scope Resolution: Try local scope first, fallback to global
		scope := res.globalScope
		if localScope, ok := res.pool.NodeScopes[ident]; ok && localScope != nil {
			scope = localScope
		}

		if sym, exists := scope.Resolve(ident.Value); exists && sym.DeclNode != nil {
			targetURI := uri
			targetDoc := doc

			// 2. Cross-File Magic: If the symbol name is a module path (e.g., math.linal.vec.Vec2),
			// we reverse-engineer the file path directly from the identifier!
			if strings.Contains(sym.Name, ".") {
				parts := strings.Split(sym.Name, ".")
				for i := len(parts); i > 0; i-- {
					relPath := strings.Join(parts[:i], string(os.PathSeparator)) + ".syn"
					if content, err := os.ReadFile(relPath); err == nil {
						// Found the external file! Convert to an absolute file:// URI
						cwd, _ := os.Getwd()
						targetURI = "file://" + cwd + string(os.PathSeparator) + relPath
						targetDoc = newDocument(targetURI, string(content))
						break
					}
				}
			}

			// Safely grab the span using a panic-recovery wrapper
			var targetSpan lexer.Span
			func() {
				defer func() { recover() }() // Catch any AST nil pointers
				targetSpan = sym.DeclNode.Span()
			}()

			// 3. Jump!
			s.sendResult(req.ID, Location{
				URI:   targetURI,
				Range: targetDoc.spanToRange(targetSpan),
			})
			return
		}
	}
	s.sendResult(req.ID, nil)
}

func findNodeAtOffset(pool *sema.TypePool, offset int) (ast.Node, sema.TypeID) {
	var bestNode ast.Node
	bestLen := -1
	var bestID sema.TypeID

	for node, typeID := range pool.NodeTypes {
		if node == nil {
			continue
		}

		// THE FIX: An Indestructible Panic Recovery Wrapper
		// While typing, the AST is often malformed. If asking a node for its Span()
		// triggers a nil pointer (like an empty Block), we catch it and ignore the node
		// instead of crashing the entire Language Server.
		func() {
			defer func() { recover() }()

			span := node.Span()
			if span.Start < 0 || span.End < span.Start || offset < span.Start || offset > span.End {
				return
			}
			spanLen := span.End - span.Start
			if bestLen == -1 || spanLen < bestLen {
				bestNode, bestLen, bestID = node, spanLen, typeID
			}
		}()
	}
	return bestNode, bestID
}

func (s *Server) handleHover(req *Request, uri string, pos Position) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}

	res := runPipeline(doc.Text)
	if res.pool == nil {
		s.sendResult(req.ID, nil)
		return
	}

	offset := doc.positionToOffset(pos)
	node, typeID := findNodeAtOffset(res.pool, offset)
	if node == nil {
		s.sendResult(req.ID, nil)
		return
	}

	t := res.pool.Types[typeID]
	md := formatTypeMarkdown(t, res.pool)
	hoverRange := doc.spanToRange(node.Span())

	s.sendResult(req.ID, HoverResult{
		Contents: MarkupContent{Kind: "markdown", Value: md},
		Range:    &hoverRange,
	})
}

// func findNodeAtOffset(pool *sema.TypePool, offset int) (ast.Node, sema.TypeID) {
// 	var bestNode ast.Node
// 	bestLen := -1
// 	var bestID sema.TypeID
//
// 	for node, typeID := range pool.NodeTypes {
// 		if node == nil {
// 			continue
// 		}
//
// 		// Safely handle specific known-dangerous nodes like empty blocks
// 		if block, ok := node.(*ast.Block); ok && block.Statements == nil && block.Value == nil {
// 			continue
// 		}
//
// 		span := node.Span()
// 		if span.Start < 0 || span.End < span.Start || offset < span.Start || offset > span.End {
// 			continue
// 		}
// 		spanLen := span.End - span.Start
// 		if bestLen == -1 || spanLen < bestLen {
// 			bestNode, bestLen, bestID = node, spanLen, typeID
// 		}
// 	}
// 	return bestNode, bestID
// }

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
		for _, vname := range vnames {
			if len(t.Variants[vname]) == 0 {
				sb.WriteString(fmt.Sprintf("    %s,\n", vname))
			} else {
				sb.WriteString(fmt.Sprintf("    %s(...),\n", vname)) // simplified for brevity
			}
		}
		sb.WriteString("}")

	case (t.Mask & sema.MaskIsStruct) != 0:
		sb.WriteString("struct " + t.Name + " {\n...}") // simplified
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
// SEMANTIC TOKENS
// ============================================================================

func (s *Server) handleSemanticTokens(req *Request, uri string) {
	doc, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, map[string]interface{}{"data": []int{}})
		return
	}

	res := runPipeline(doc.Text)
	var tokens []int

	// 0: type, 1: struct, 2: enum, 3: function, 4: variable, 5: keyword, 6: operator
	prevLine, prevCol := 0, 0

	for _, tok := range res.tokens {
		tokType := -1

		switch tok.Type {
		case lexer.STRUCT, lexer.ENUM, lexer.IMPL, lexer.FNC, lexer.RET,
			lexer.DEFER, lexer.BRK, lexer.IF, lexer.ELIF, lexer.ELSE,
			lexer.MATCH, lexer.LOOP, lexer.AS, lexer.TRUE, lexer.FALSE:
			tokType = 5

		// 2. Operators
		case lexer.ASSIGN, lexer.DECL_ASSIGN, lexer.MUT_ASSIGN, lexer.PLUS_ASSIGN,
			lexer.MIN_ASSIGN, lexer.MUL_ASSIGN, lexer.DIV_ASSIGN, lexer.MOD_ASSIGN,
			lexer.BIT_AND_ASSIGN, lexer.BIT_OR_ASSIGN, lexer.BIT_XOR_ASSIGN,
			lexer.LSHIFT_ASSIGN, lexer.RSHIFT_ASSIGN, lexer.PLUS, lexer.MINUS,
			lexer.ASTERISK, lexer.SLASH, lexer.MOD, lexer.BANG, lexer.TILDE,
			lexer.AMPERS, lexer.PIPE, lexer.CARET, lexer.QUESTION, lexer.LSHIFT,
			lexer.RSHIFT, lexer.AND, lexer.OR, lexer.EQ, lexer.NOT_EQ, lexer.LT,
			lexer.LTE, lexer.GT, lexer.GTE, lexer.ARROW, lexer.RANGE, lexer.DOT:
			tokType = 6

		// 3. Identifiers
		case lexer.IDENT:
			tokType = 4 // Default variable
			// Cross-reference with evaluator to color types/functions accurately!
			// THE FIX: Changed tok.Value to tok.Literal
			if sym, exists := res.globalScope.Resolve(tok.Literal); exists {
				if int(sym.TypeID) < len(res.pool.Types) {
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

		// 4. Strings & Numbers (Optional, but makes it robust)
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
	res := runPipeline(doc.Text)

	if offset > 0 {
		cursor := offset
		for cursor > 0 && isIdentChar(doc.Text[cursor-1]) {
			cursor--
		}
		if cursor > 0 && doc.Text[cursor-1] == '.' {
			items := s.dotCompletion(doc.Text, cursor-1, res)
			if items != nil {
				s.sendResult(req.ID, CompletionList{IsIncomplete: false, Items: items})
				return
			}
		}
	}

	s.sendResult(req.ID, CompletionList{IsIncomplete: false, Items: s.generalCompletion(res)})
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

func (s *Server) generalCompletion(res *pipelineResult) []CompletionItem {
	items := make([]CompletionItem, len(synoviumKeywords))
	copy(items, synoviumKeywords)
	if res.globalScope == nil || res.pool == nil {
		return items
	}

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
	return items
}

// newDocument is a helper to instantly index a file's line offsets for O(log N) lookups
func newDocument(uri, text string) *Document {
	offsets := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return &Document{
		Text:        text,
		LineOffsets: offsets,
	}
}
