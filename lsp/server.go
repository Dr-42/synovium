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

// Request is the inbound message from the editor.
// ID is a pointer so we can distinguish "no ID" (notifications) from ID=0.
type Request struct {
	RPC    string           `json:"jsonrpc"`
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params,omitempty"`
}

// Response is sent back to the editor for requests (has an ID).
type Response struct {
	RPC    string           `json:"jsonrpc"`
	ID     *json.RawMessage `json:"id"`
	Result interface{}      `json:"result,omitempty"`
	Error  interface{}      `json:"error,omitempty"`
}

// Notification is sent to the editor unprompted (no ID), e.g. publishDiagnostics.
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

// Diagnostic severity constants (LSP spec).
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

// --- Hover ---

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`  // "plaintext" or "markdown"
	Value string `json:"value"`
}

// --- Completion ---

// CompletionItemKind mirrors the LSP spec numeric values.
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
	Documentation    string `json:"documentation,omitempty"`
	InsertText       string `json:"insertText,omitempty"`
	InsertTextFormat int    `json:"insertTextFormat,omitempty"` // 1=plaintext 2=snippet
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// ============================================================================
// DOCUMENT STORE
// ============================================================================

// docStore is a thread-safe map of URI -> full file text.
// The LSP server receives full-file syncs (changeType=1) so we just overwrite.
type docStore struct {
	mu   sync.RWMutex
	docs map[string]string
}

func newDocStore() *docStore {
	return &docStore{docs: make(map[string]string)}
}

func (d *docStore) set(uri, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.docs[uri] = text
}

func (d *docStore) get(uri string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.docs[uri]
	return t, ok
}

func (d *docStore) del(uri string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.docs, uri)
}

// ============================================================================
// PIPELINE
// ============================================================================

// pipelineResult bundles everything produced by a full lex→parse→sema pass.
type pipelineResult struct {
	pool        *sema.TypePool
	globalScope *sema.Scope
	sortedDecls []ast.Decl
	parseErrors []string
	semaErrors  []string
}

// runPipeline runs the Synovium compiler front-end up to (and including) semantic
// analysis. The JIT callback is stubbed so we never shell out to Clang on
// keystrokes — the TAST type information is fully populated without it.
func runPipeline(code string) *pipelineResult {
	res := &pipelineResult{}

	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseSourceFile()
	res.parseErrors = p.Errors()

	pool := sema.NewTypePool()
	globalScope := sema.NewScope(nil)
	evaluator := sema.NewEvaluator(pool, code)
	evaluator.GlobalDecls = program.Declarations

	// ── JIT STUB ──────────────────────────────────────────────────────────────
	// Never call Clang in the LSP hot path. Return zeroed bytes whose length
	// matches the target type's size — structurally valid, value is a lie, but
	// the TAST type stamps are all we need for hover and completion.
	evaluator.JITCallback = func(
		expr ast.Expr,
		targetType sema.TypeID,
		pool *sema.TypePool,
		envScope *sema.Scope,
		globalDecls []ast.Decl,
	) ([]byte, error) {
		size := pool.Types[targetType].TrueSizeBits / 8
		if size == 0 {
			size = 1
		}
		return make([]byte, size), nil
	}
	// ─────────────────────────────────────────────────────────────────────────

	evaluator.InjectBuiltins(globalScope)

	dag := sema.NewDAG(globalScope)
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

// ============================================================================
// KEYWORD COMPLETION ITEMS  (defined here so Part 5 can reference them)
// ============================================================================

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
	{Label: "brk", Kind: CIKKeyword, InsertTextFormat: 1},
	{Label: "defer", Kind: CIKKeyword, InsertText: "defer { $0 }", InsertTextFormat: 2},
	{Label: "as", Kind: CIKKeyword, InsertTextFormat: 1},
	{Label: "type", Kind: CIKKeyword, InsertTextFormat: 1},
}

// ============================================================================
// POSITION / SPAN UTILITIES
// ============================================================================

// positionToOffset converts a 0-indexed LSP (line, character) into a byte offset.
func positionToOffset(code string, pos Position) int {
	line, col := 0, 0
	for i := 0; i < len(code); i++ {
		if line == pos.Line && col == pos.Character {
			return i
		}
		if code[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return len(code)
}

// offsetToPosition converts a byte offset into a 0-indexed LSP Position.
func offsetToPosition(code string, offset int) Position {
	line, col := 0, 0
	for i := 0; i < offset && i < len(code); i++ {
		if code[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return Position{Line: line, Character: col}
}

// spanToRange converts a lexer byte-span into an LSP Range.
func spanToRange(code string, span lexer.Span) Range {
	return Range{
		Start: offsetToPosition(code, span.Start),
		End:   offsetToPosition(code, span.End),
	}
}

// isIdentChar reports whether c can appear inside a Synovium identifier.
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_'
}

// Ensure sort is used (it's used in Part 4; this silences the import if Part 1
// is ever compiled standalone during development).
var _ = sort.Strings
var _ = fmt.Sprintf
var _ = strconv.Atoi
var _ = strings.Split
var _ = bufio.NewReader
var _ = io.EOF
var _ = log.New

// ============================================================================
// SERVER  (Part 2 of 5)
// ============================================================================
//
// Contains:
//   - Server struct
//   - Start()  — entry point called from main.go
//   - run()    — the main read loop
//   - dispatch() — routes every incoming method to its handler

// Server holds all mutable server state.
type Server struct {
	docs   *docStore
	writer *bufio.Writer
	logger *log.Logger
}

// Start is called by main.go when the user runs `synovium lsp`.
// It boots the server and blocks until stdin is closed.
func Start() {
	logFile, err := os.OpenFile("synovium_lsp.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		// Fall back to stderr so we at least get *something* if the log file fails.
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

// run reads Content-Length-framed JSON-RPC messages from stdin forever.
func (s *Server) run() {
	reader := bufio.NewReader(os.Stdin)

	for {
		// ── Step 1: read HTTP-style headers until blank line ──────────────────
		contentLength := -1
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					s.logger.Println("stdin read error:", err)
				}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break // blank line signals end-of-headers
			}
			if strings.HasPrefix(line, "Content-Length:") {
				lenStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
				contentLength, _ = strconv.Atoi(lenStr)
			}
		}

		if contentLength <= 0 {
			continue
		}

		// ── Step 2: read exactly contentLength bytes ──────────────────────────
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, body); err != nil {
			s.logger.Println("body read error:", err)
			return
		}

		// ── Step 3: unmarshal and dispatch ────────────────────────────────────
		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			s.logger.Println("unmarshal error:", err)
			continue
		}

		s.logger.Printf("← %s", req.Method)
		s.dispatch(&req)
	}
}

// dispatch routes an incoming request or notification to the correct handler.
func (s *Server) dispatch(req *Request) {
	switch req.Method {

	// ── Lifecycle ─────────────────────────────────────────────────────────────
	case "initialize":
		s.handleInitialize(req)

	case "initialized":
		// Client acknowledgement — no response required.

	case "shutdown":
		// Client is about to send "exit". Reply with null result.
		s.sendResult(req.ID, nil)

	case "exit":
		os.Exit(0)

	// ── Text Document Synchronisation ─────────────────────────────────────────
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		json.Unmarshal(req.Params, &p)
		s.docs.set(p.TextDocument.URI, p.TextDocument.Text)
		// Diagnostics are slow (full pipeline); run them off the dispatch goroutine.
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
			// Full-sync mode: take the last change (should only ever be one).
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
		// Clear any lingering diagnostics for this file in the editor.
		go s.sendDiagnostics(p.TextDocument.URI, []Diagnostic{})

	// ── Language Features ─────────────────────────────────────────────────────
	case "textDocument/hover":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		json.Unmarshal(req.Params, &p)
		s.handleHover(req, p.TextDocument.URI, p.Position)

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
		// For any method we don't support, reply with "method not found" if it
		// was a request (has an ID). Notifications (no ID) are silently ignored.
		if req.ID != nil {
			s.sendError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

// ============================================================================
// TRANSPORT HELPERS
// ============================================================================

// sendRaw JSON-marshals v and writes it as a Content-Length-framed message.
func (s *Server) sendRaw(v interface{}) {
	body, err := json.Marshal(v)
	if err != nil {
		s.logger.Println("marshal error:", err)
		return
	}
	fmt.Fprintf(s.writer, "Content-Length: %d\r\n\r\n", len(body))
	s.writer.Write(body)
	s.writer.Flush()
}

// sendResult replies to a request with a success result.
func (s *Server) sendResult(id *json.RawMessage, result interface{}) {
	s.sendRaw(Response{RPC: "2.0", ID: id, Result: result})
}

// sendError replies to a request with a JSON-RPC error object.
func (s *Server) sendError(id *json.RawMessage, code int, msg string) {
	s.sendRaw(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]interface{}{"code": code, "message": msg},
	})
}

// sendDiagnostics pushes a publishDiagnostics notification to the editor.
func (s *Server) sendDiagnostics(uri string, diagnostics []Diagnostic) {
	if diagnostics == nil {
		diagnostics = []Diagnostic{} // JSON must be [] not null
	}
	s.sendRaw(Notification{
		RPC:    "2.0",
		Method: "textDocument/publishDiagnostics",
		Params: PublishDiagnosticsParams{URI: uri, Diagnostics: diagnostics},
	})
	s.logger.Printf("→ publishDiagnostics %s (%d items)", uri, len(diagnostics))
}

// ============================================================================
// INITIALIZE  (Part 3 of 5)
// ============================================================================
//
// Contains:
//   - handleInitialize  — capability negotiation
//   - publishDiagnostics — runs the full pipeline and sends errors to editor
//   - parseSemaError    — recovers line/col/squiggly-length from formatted errors

// handleInitialize responds to the editor's opening handshake with the set of
// capabilities this server supports.
func (s *Server) handleInitialize(req *Request) {
	s.sendResult(req.ID, map[string]interface{}{
		"capabilities": map[string]interface{}{
			// Full-file sync: editor sends the entire document on every change.
			"textDocumentSync": map[string]interface{}{
				"openClose": true,
				"change":    1,
			},
			// Hover over any identifier to see its resolved type.
			"hoverProvider": true,
			// Completion triggered by '.' and ':'.
			"completionProvider": map[string]interface{}{
				"triggerCharacters": []string{".", ":"},
				"resolveProvider":   false,
			},
		},
		"serverInfo": map[string]interface{}{
			"name":    "synovium-lsp",
			"version": "0.0.1",
		},
	})
}

// ============================================================================
// DIAGNOSTICS
// ============================================================================

// publishDiagnostics runs the full Synovium front-end pipeline over `code` and
// pushes any parse or semantic errors to the editor as LSP diagnostics.
func (s *Server) publishDiagnostics(uri, code string) {
	var diagnostics []Diagnostic

	res := runPipeline(code)

	// ── 1. Parse errors ───────────────────────────────────────────────────────
	// The parser reports errors as plain strings, sometimes containing
	// "at line N". We extract the line number when present.
	for _, msg := range res.parseErrors {
		clean := ansiRegex.ReplaceAllString(msg, "")

		line := 0
		if parts := strings.Split(clean, "at line "); len(parts) >= 2 {
			numStr := strings.TrimSpace(parts[1])
			// The number might be followed by more text; grab just the digits.
			end := 0
			for end < len(numStr) && numStr[end] >= '0' && numStr[end] <= '9' {
				end++
			}
			if end > 0 {
				line, _ = strconv.Atoi(numStr[:end])
				line-- // LSP lines are 0-indexed
				if line < 0 {
					line = 0
				}
			}
		}

		diagnostics = append(diagnostics, Diagnostic{
			Range: Range{
				Start: Position{Line: line, Character: 0},
				End:   Position{Line: line, Character: 9999},
			},
			Severity: SeverityError,
			Source:   "synovium(parse)",
			Message:  "Syntax Error: " + clean,
		})
	}

	// ── 2. Semantic / DAG errors ──────────────────────────────────────────────
	// The evaluator formats errors as multi-line ANSI strings with an embedded
	// "--> line L:C" coordinate and a "^^^^" squiggly underline.
	// parseSemaError recovers all of that into a structured Diagnostic.
	for _, msg := range res.semaErrors {
		diagnostics = append(diagnostics, parseSemaError(msg))
	}

	s.sendDiagnostics(uri, diagnostics)
}

// parseSemaError recovers a structured Diagnostic from the evaluator's
// Rust-style formatted error string.
//
// The evaluator emits messages shaped like:
//
//	Error: <message text>
//	  --> line L:C
//	L-1 | <context line>
//	  L | <error source line>
//	    | ^^^^^^^^^^^^^^^^^^^    ← squiggly; length = highlight width
//	L+1 | <context line>
func parseSemaError(raw string) Diagnostic {
	clean := ansiRegex.ReplaceAllString(raw, "")
	lines := strings.Split(clean, "\n")

	message := ""
	diagLine := 0
	diagCol := 0
	squigglyLen := 1

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)

		// "Error: <msg>" — first occurrence wins.
		if strings.HasPrefix(trimmed, "Error:") && message == "" {
			message = strings.TrimSpace(strings.TrimPrefix(trimmed, "Error:"))
			continue
		}

		// "--> line L:C"
		if strings.HasPrefix(trimmed, "--> line ") {
			coords := strings.TrimPrefix(trimmed, "--> line ")
			fmt.Sscanf(coords, "%d:%d", &diagLine, &diagCol)
			diagLine-- // convert to 0-indexed
			diagCol--
			if diagLine < 0 {
				diagLine = 0
			}
			if diagCol < 0 {
				diagCol = 0
			}
			continue
		}

		// The squiggly line: after the "|" separator it contains only spaces/tabs
		// followed by one or more "^" characters.
		if strings.Contains(trimmed, "^") {
			// Strip the leading "| " or "|" that the formatter adds.
			after := trimmed
			if idx := strings.Index(after, "| "); idx != -1 {
				after = after[idx+2:]
			} else if idx := strings.Index(after, "|"); idx != -1 {
				after = after[idx+1:]
			}
			// Count only the "^" characters (ignore leading whitespace).
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

	// Fallback: if we couldn't parse a message, use the whole cleaned string.
	if message == "" {
		message = strings.TrimSpace(clean)
	}

	return Diagnostic{
		Range: Range{
			Start: Position{Line: diagLine, Character: diagCol},
			End:   Position{Line: diagLine, Character: diagCol + squigglyLen},
		},
		Severity: SeverityError,
		Source:   "synovium",
		Message:  message,
	}
}

// ============================================================================
// HOVER  (Part 4 of 5)
// ============================================================================
//
// Contains:
//   - handleHover        — top-level hover request handler
//   - findNodeAtOffset   — walks pool.NodeTypes for the innermost node at cursor
//   - formatTypeMarkdown — renders a UniversalType as a Markdown code block

// handleHover responds to textDocument/hover by finding the TAST node under
// the cursor and rendering its resolved type as a Markdown snippet.
func (s *Server) handleHover(req *Request, uri string, pos Position) {
	code, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, nil)
		return
	}

	offset := positionToOffset(code, pos)

	res := runPipeline(code)
	if res.pool == nil {
		s.sendResult(req.ID, nil)
		return
	}

	node, typeID := findNodeAtOffset(res.pool, offset)
	if node == nil {
		s.sendResult(req.ID, nil)
		return
	}

	t := res.pool.Types[typeID]
	md := formatTypeMarkdown(t, res.pool)

	hoverRange := spanToRange(code, node.Span())
	s.sendResult(req.ID, HoverResult{
		Contents: MarkupContent{Kind: "markdown", Value: md},
		Range:    &hoverRange,
	})
}

// findNodeAtOffset searches pool.NodeTypes for the innermost node whose byte
// span contains `offset`. "Innermost" means the node with the shortest span —
// e.g. an Identifier inside a CallExpr inside a Block.
func findNodeAtOffset(pool *sema.TypePool, offset int) (ast.Node, sema.TypeID) {
	var bestNode ast.Node
	bestLen := -1
	var bestID sema.TypeID

	for node, typeID := range pool.NodeTypes {
		span := node.Span()

		// Guard against invalid or zero-width spans from generated nodes.
		if span.Start < 0 || span.End < span.Start {
			continue
		}
		// The cursor must be within the span (inclusive on both ends).
		if offset < span.Start || offset > span.End {
			continue
		}

		spanLen := span.End - span.Start
		if bestLen == -1 || spanLen < bestLen {
			bestNode = node
			bestLen = spanLen
			bestID = typeID
		}
	}

	return bestNode, bestID
}

// formatTypeMarkdown renders a UniversalType as a fenced Synovium code block
// suitable for display in an editor hover popup.
func formatTypeMarkdown(t sema.UniversalType, pool *sema.TypePool) string {
	var sb strings.Builder
	sb.WriteString("```synovium\n")

	switch {

	// ── Function ──────────────────────────────────────────────────────────────
	case (t.Mask & sema.MaskIsFunction) != 0:
		var params []string
		for _, pID := range t.FuncParams {
			if int(pID) < len(pool.Types) {
				params = append(params, pool.Types[pID].Name)
			}
		}
		retName := "void"
		if int(t.FuncReturn) < len(pool.Types) {
			if n := pool.Types[t.FuncReturn].Name; n != "void" {
				retName = n
			}
		}
		// Strip the "_signature" suffix that the evaluator appends internally.
		name := strings.TrimSuffix(t.Name, "_signature")
		if retName == "void" {
			sb.WriteString(fmt.Sprintf("fnc %s(%s)", name, strings.Join(params, ", ")))
		} else {
			sb.WriteString(fmt.Sprintf("fnc %s(%s) = %s", name, strings.Join(params, ", "), retName))
		}

	// ── Enum (tagged union) ───────────────────────────────────────────────────
	case (t.Mask&sema.MaskIsStruct) != 0 && len(t.Variants) > 0:
		sb.WriteString("enum " + t.Name + " {\n")

		// Sort variant names so the output is deterministic across runs.
		vnames := make([]string, 0, len(t.Variants))
		for k := range t.Variants {
			vnames = append(vnames, k)
		}
		sort.Strings(vnames)

		for _, vname := range vnames {
			payloads := t.Variants[vname]
			if len(payloads) == 0 {
				sb.WriteString(fmt.Sprintf("    %s,\n", vname))
			} else {
				pts := make([]string, 0, len(payloads))
				for _, pid := range payloads {
					if int(pid) < len(pool.Types) {
						pts = append(pts, pool.Types[pid].Name)
					}
				}
				sb.WriteString(fmt.Sprintf("    %s(%s),\n", vname, strings.Join(pts, ", ")))
			}
		}
		sb.WriteString("}")

	// ── Struct ────────────────────────────────────────────────────────────────
	case (t.Mask & sema.MaskIsStruct) != 0:
		sb.WriteString("struct " + t.Name)
		if len(t.Fields) > 0 {
			sb.WriteString(" {\n")

			// Sort fields by their declared index (FieldIndices) so the output
			// mirrors the declaration order, not Go's map iteration order.
			type fieldEntry struct {
				name   string
				idx    int
				typeID sema.TypeID
			}
			entries := make([]fieldEntry, 0, len(t.Fields))
			for fname, fid := range t.Fields {
				idx := 0
				if t.FieldIndices != nil {
					idx = t.FieldIndices[fname]
				}
				entries = append(entries, fieldEntry{fname, idx, fid})
			}
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].idx < entries[j].idx
			})

			for _, e := range entries {
				typeName := ""
				if int(e.typeID) < len(pool.Types) {
					typeName = pool.Types[e.typeID].Name
				}
				sb.WriteString(fmt.Sprintf("    %s: %s,\n", e.name, typeName))
			}
			sb.WriteString("}")
		}

	// ── Pointer ───────────────────────────────────────────────────────────────
	case (t.Mask & sema.MaskIsPointer) != 0:
		base := ""
		if int(t.BaseType) < len(pool.Types) {
			base = pool.Types[t.BaseType].Name
		}
		sb.WriteString("*" + base)

	// ── Array / Slice ─────────────────────────────────────────────────────────
	case (t.Mask & sema.MaskIsArray) != 0:
		base := ""
		if int(t.BaseType) < len(pool.Types) {
			base = pool.Types[t.BaseType].Name
		}
		if t.Capacity == 0 {
			sb.WriteString(fmt.Sprintf("[%s; :]", base)) // slice
		} else {
			sb.WriteString(fmt.Sprintf("[%s; %d]", base, t.Capacity)) // fixed array
		}

	// ── Primitive / everything else ───────────────────────────────────────────
	default:
		sb.WriteString(t.Name)
	}

	sb.WriteString("\n```")

	// ── Append method list ────────────────────────────────────────────────────
	if len(t.Methods) > 0 {
		sb.WriteString("\n\n**Methods:**\n")

		mnames := make([]string, 0, len(t.Methods))
		for k := range t.Methods {
			mnames = append(mnames, k)
		}
		sort.Strings(mnames)

		for _, mname := range mnames {
			mid := t.Methods[mname]
			if int(mid) >= len(pool.Types) {
				continue
			}
			mt := pool.Types[mid]

			mparams := make([]string, 0, len(mt.FuncParams))
			for _, pid := range mt.FuncParams {
				if int(pid) < len(pool.Types) {
					mparams = append(mparams, pool.Types[pid].Name)
				}
			}

			mret := ""
			if int(mt.FuncReturn) < len(pool.Types) {
				if rn := pool.Types[mt.FuncReturn].Name; rn != "void" {
					mret = " = " + rn
				}
			}

			sb.WriteString(fmt.Sprintf("- `%s(%s)%s`\n", mname, strings.Join(mparams, ", "), mret))
		}
	}

	return sb.String()
}

// ============================================================================
// COMPLETION  (Part 5 of 5)
// ============================================================================
//
// Contains:
//   - handleCompletion  — top-level completion request handler
//   - dotCompletion     — field / method / variant completions for `expr.`
//   - generalCompletion — keywords + all globally resolved symbols

// handleCompletion responds to textDocument/completion.
//
// Strategy:
//  1. Look at the character immediately before the cursor (skipping any partial
//     identifier the user is in the middle of typing).
//  2. If that character is '.', delegate to dotCompletion for member access.
//  3. Otherwise, return the global keyword list + all top-level scope symbols.
func (s *Server) handleCompletion(req *Request, uri string, pos Position) {
	code, ok := s.docs.get(uri)
	if !ok {
		s.sendResult(req.ID, CompletionList{})
		return
	}

	offset := positionToOffset(code, pos)
	res := runPipeline(code)

	// ── Dot-completion detection ──────────────────────────────────────────────
	//
	// Walk backwards from the cursor past any partial identifier being typed,
	// then check whether the next character is '.'.
	//
	// Example:  "v.get_"  — cursor is after 'get_'
	//           offset points here: v.get_|
	//   We step back past "get_" → land on '.' → dot completion on "v".
	if offset > 0 {
		cursor := offset
		// Step back over the partial identifier (if any).
		for cursor > 0 && isIdentChar(code[cursor-1]) {
			cursor--
		}
		// Now check for the dot.
		if cursor > 0 && code[cursor-1] == '.' {
			items := s.dotCompletion(code, cursor-1, res)
			if items != nil {
				s.sendResult(req.ID, CompletionList{IsIncomplete: false, Items: items})
				return
			}
		}
	}

	// ── General completion ────────────────────────────────────────────────────
	s.sendResult(req.ID, CompletionList{
		IsIncomplete: false,
		Items:        s.generalCompletion(res),
	})
}

// dotCompletion resolves the expression to the left of a '.' at `dotOffset`
// and returns completion items for all accessible members of that type.
//
// It handles simple identifiers ("v.") and simple dot-chains ("std.io.").
func (s *Server) dotCompletion(code string, dotOffset int, res *pipelineResult) []CompletionItem {
	if res.pool == nil || res.globalScope == nil {
		return nil
	}

	// ── Extract the identifier / chain to the left of the dot ────────────────
	end := dotOffset // exclusive — the dot itself
	start := end
	for start > 0 && (isIdentChar(code[start-1]) || code[start-1] == '.') {
		start--
	}
	chain := code[start:end]
	if chain == "" {
		return nil
	}

	// ── Resolve the chain in the global scope ─────────────────────────────────
	sym, exists := res.globalScope.Resolve(chain)
	if !exists || !sym.IsResolved {
		return nil
	}
	if int(sym.TypeID) >= len(res.pool.Types) {
		return nil
	}

	t := res.pool.Types[sym.TypeID]

	// Auto-deref: if the variable is a pointer, peel it back to the base type.
	if (t.Mask&sema.MaskIsPointer) != 0 && int(t.BaseType) < len(res.pool.Types) {
		t = res.pool.Types[t.BaseType]
	}

	var items []CompletionItem

	// ── Struct fields ─────────────────────────────────────────────────────────
	for fname, fid := range t.Fields {
		detail := ""
		if int(fid) < len(res.pool.Types) {
			detail = res.pool.Types[fid].Name
		}
		items = append(items, CompletionItem{
			Label:  fname,
			Kind:   CIKField,
			Detail: detail,
		})
	}

	// ── Impl methods ──────────────────────────────────────────────────────────
	for mname, mid := range t.Methods {
		detail := "fnc"
		if int(mid) < len(res.pool.Types) {
			mt := res.pool.Types[mid]
			if int(mt.FuncReturn) < len(res.pool.Types) {
				rn := res.pool.Types[mt.FuncReturn].Name
				if rn != "void" {
					detail = "fnc = " + rn
				}
			}
		}
		items = append(items, CompletionItem{
			Label:  mname,
			Kind:   CIKMethod,
			Detail: detail,
		})
	}

	// ── Enum variants ─────────────────────────────────────────────────────────
	for vname, payloads := range t.Variants {
		detail := ""
		if len(payloads) > 0 {
			pts := make([]string, 0, len(payloads))
			for _, pid := range payloads {
				if int(pid) < len(res.pool.Types) {
					pts = append(pts, res.pool.Types[pid].Name)
				}
			}
			detail = "(" + strings.Join(pts, ", ") + ")"
		}
		items = append(items, CompletionItem{
			Label:  vname,
			Kind:   CIKEnumMember,
			Detail: detail,
		})
	}

	// Return nil (not an empty slice) when the type has no members at all,
	// so the caller can fall back to general completion.
	if len(items) == 0 {
		return nil
	}

	return items
}

// generalCompletion returns the full keyword list plus every resolved symbol
// in the global scope with an appropriate CompletionItemKind icon.
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

		var kind int
		switch {
		case (t.Mask & sema.MaskIsFunction) != 0:
			kind = CIKFunction
		case (t.Mask&sema.MaskIsStruct) != 0 && len(t.Variants) > 0:
			kind = CIKEnum
		case (t.Mask & sema.MaskIsStruct) != 0:
			kind = CIKStruct
		case (t.Mask & sema.MaskIsMeta) != 0:
			kind = CIKKeyword // primitive type names like i32, f64, bln …
		default:
			kind = CIKVariable
		}

		items = append(items, CompletionItem{
			Label:  name,
			Kind:   kind,
			Detail: t.Name,
		})
	}

	return items
}
