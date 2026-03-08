package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"synovium/codegen"
	"synovium/lexer"
	"synovium/parser"
	"synovium/sema"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"` // <-- NEW: Tell Neovim who sent this!
	Message  string `json:"message"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Notification is used to send diagnostics to Neovim (it doesn't have an ID)
type Notification struct {
	RPC    string      `json:"jsonrpc"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// The base JSON-RPC structure Neovim expects
type Request struct {
	RPC    string          `json:"jsonrpc"`
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	RPC    string      `json:"jsonrpc"`
	ID     int         `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  interface{} `json:"error,omitempty"`
}

func Start() {
	// We MUST log to a file. We cannot use fmt.Println,
	// because stdout is exclusively reserved for talking to Neovim!
	logFile, err := os.OpenFile("synovium_lsp.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return
	}
	defer logFile.Close()
	logger := log.New(logFile, "[LSP] ", log.Ltime)

	logger.Println("Synovium Language Server Booting...")

	reader := bufio.NewReader(os.Stdin)

	for {
		// 1. Read the HTTP-style Header (Content-Length: X)
		var length int
		for {
			header, err := reader.ReadString('\n')
			if err != nil {
				logger.Println("Connection closed by client.")
				return
			}
			header = strings.TrimSpace(header)
			if header == "" {
				break // End of headers
			}
			if strings.HasPrefix(header, "Content-Length:") {
				lenStr := strings.TrimSpace(strings.TrimPrefix(header, "Content-Length:"))
				length, _ = strconv.Atoi(lenStr)
			}
		}

		// 2. Read the exact JSON payload length
		body := make([]byte, length)
		_, err = io.ReadFull(reader, body)
		if err != nil {
			logger.Println("Failed to read body:", err)
			return
		}

		var req Request
		json.Unmarshal(body, &req)

		logger.Printf("Received: %s", req.Method)

		// 3. The Handshake Protocol
		if req.Method == "initialize" {
			logger.Println("Handshaking with Neovim...")

			result := map[string]interface{}{
				"capabilities": map[string]interface{}{
					"textDocumentSync": map[string]interface{}{
						"openClose": true,
						"change":    1, // 1 = Full sync (entire file is sent on change)
					},
					"hoverProvider": true,
				},
			}
			sendResponse(req.ID, result, logger)
		}

		// 4. File Opened
		if req.Method == "textDocument/didOpen" {
			var params struct {
				TextDocument struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"textDocument"`
			}
			json.Unmarshal(req.Params, &params)
			runDiagnostics(params.TextDocument.URI, params.TextDocument.Text, logger)
		}

		// 5. File Modified (Keystrokes)
		if req.Method == "textDocument/didChange" {
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			json.Unmarshal(req.Params, &params)
			if len(params.ContentChanges) > 0 {
				runDiagnostics(params.TextDocument.URI, params.ContentChanges[0].Text, logger)
			}
		}
	}
}

// sendResponse wraps the payload in headers and fires it back to Neovim over stdout
func sendResponse(id int, result interface{}, logger *log.Logger) {
	resp := Response{
		RPC:    "2.0",
		ID:     id,
		Result: result,
	}

	body, _ := json.Marshal(resp)
	reply := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), string(body))

	fmt.Print(reply) // Fire to Neovim
	logger.Printf("Responded to ID %d", id)
}

func runDiagnostics(uri string, code string, logger *log.Logger) {
	var diagnostics []Diagnostic

	// 1. Syntactic Analysis
	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseSourceFile()

	if len(p.Errors()) > 0 {
		for _, errMsg := range p.Errors() {
			line := 0
			if parts := strings.Split(errMsg, "at line "); len(parts) == 2 {
				line, _ = strconv.Atoi(parts[1])
			}

			line-- // LSP lines are 0-indexed
			if line < 0 {
				line = 0
			}

			// Strip ANSI codes just in case
			cleanMsg := ansiRegex.ReplaceAllString(strings.Split(errMsg, "at line ")[0], "")

			diagnostics = append(diagnostics, Diagnostic{
				Range: Range{
					Start: Position{Line: line, Character: 0},
					End:   Position{Line: line, Character: 99},
				},
				Severity: 1,
				Source:   "synovium",
				Message:  "Syntax Error: " + cleanMsg,
			})
		}
		sendDiagnostics(uri, diagnostics, logger)
		return
	}

	// 2. Semantic Analysis & DAG
	pool := sema.NewTypePool()
	globalScope := sema.NewScope(nil)
	evaluator := sema.NewEvaluator(pool, code)
	evaluator.GlobalDecls = program.Declarations

	// THE FIX: Initialize the JIT Engine for Comptime Blocks!
	evaluator.JITCallback = codegen.RunJIT

	evaluator.InjectBuiltins(globalScope)

	dag := sema.NewDAG(globalScope)
	sortedDecls, err := dag.BuildAndSort(program)
	if err != nil {
		cleanMsg := ansiRegex.ReplaceAllString(err.Error(), "")
		diagnostics = append(diagnostics, Diagnostic{
			Range:    Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 99}},
			Severity: 1,
			Source:   "synovium",
			Message:  "DAG Error: " + cleanMsg,
		})
		sendDiagnostics(uri, diagnostics, logger)
		return
	}

	for _, decl := range sortedDecls {
		evaluator.Evaluate(decl, globalScope)
	}

	// 3. Report Semantic Errors
	if len(evaluator.Errors) > 0 {
		for _, errMsg := range evaluator.Errors {
			line, col := 0, 0
			cleanMsg := errMsg

			if parts := strings.Split(errMsg, "--> line "); len(parts) == 2 {
				cleanMsg = strings.TrimSpace(parts[0])
				coords := strings.Split(strings.TrimSpace(parts[1]), ":")
				if len(coords) >= 2 {
					line, _ = strconv.Atoi(coords[0])
					col, _ = strconv.Atoi(coords[1])
				}
			}

			// Strip ANSI codes from the Semantic Evaluator!
			cleanMsg = ansiRegex.ReplaceAllString(cleanMsg, "")

			line-- // LSP 0-index
			col--  // LSP 0-index
			if line < 0 {
				line = 0
			}
			if col < 0 {
				col = 0
			}

			diagnostics = append(diagnostics, Diagnostic{
				Range: Range{
					Start: Position{Line: line, Character: col},
					End:   Position{Line: line, Character: col + 5},
				},
				Severity: 1,
				Source:   "synovium",
				Message:  cleanMsg,
			})
		}
	}

	// Send the final array (even if it's empty, this clears old errors!)
	sendDiagnostics(uri, diagnostics, logger)
}

func sendDiagnostics(uri string, diagnostics []Diagnostic, logger *log.Logger) {
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}

	notif := Notification{
		RPC:    "2.0",
		Method: "textDocument/publishDiagnostics",
		Params: PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: diagnostics,
		},
	}

	body, _ := json.Marshal(notif)
	reply := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), string(body))
	fmt.Print(reply)
	logger.Printf("Published %d diagnostics to %s", len(diagnostics), uri)
}
