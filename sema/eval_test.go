package sema

import (
	"fmt"
	"strings"
	"synovium/lexer"
	"synovium/parser"
	"testing"
)

// runSemaPipeline compiles a Synovium snippet strictly up to the Semantic Phase.
func runSemaPipeline(input string) (*Evaluator, *Scope, error) {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseSourceFile()

	if len(p.Errors()) != 0 {
		return nil, nil, fmt.Errorf("parse failed: %v", p.Errors())
	}

	pool := NewTypePool()
	globalScope := NewScope(nil)
	evaluator := NewEvaluator(pool)
	evaluator.InjectBuiltins(globalScope)

	dag := NewDAG(globalScope)
	sortedDecls, err := dag.BuildAndSort(program)
	if err != nil {
		return nil, nil, fmt.Errorf("DAG failed: %v", err)
	}

	for _, decl := range sortedDecls {
		evaluator.Evaluate(decl, globalScope)
	}

	return evaluator, globalScope, nil
}

func TestSemanticRules(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedError string // Leave empty "" if we expect it to pass perfectly
	}{
		{
			name:          "Valid Implicit Promotion",
			input:         `fnc main() { x : f64 ~= 10.5; y : i32 = 2; x += y; }`,
			expectedError: "",
		},
		{
			name:          "Catch Invalid Mutation",
			input:         `fnc main() { x : i32 = 10; x += 5; }`,
			expectedError: "cannot mutate immutable variable 'x'",
		},
		{
			name:          "Catch Type Mismatch",
			input:         `fnc main() { x : i32 = "hello"; }`,
			expectedError: "type mismatch",
		},
		{
			name:          "Valid Relational Operation",
			input:         `fnc main() { is_valid : bln = 5 < 10; }`,
			expectedError: "",
		},

		// 2. GENERICS & MONOMORPHIZATION
		{
			name:          "Valid Generic Monomorphization",
			input:         `fnc id(T: type, v: T) = T { ret v; } fnc main() { x : f64 = id(f64, 3.14); }`,
			expectedError: "",
		},
		{
			name:          "Catch Invalid Generic Argument",
			input:         `fnc id(T: type, v: T) = T { ret v; } fnc main() { x : f64 = id(i32, 3.14); }`,
			expectedError: "argument type mismatch in generic instantiation",
		},

		// 3. STRUCTS & METHODS (AUTO-DEREFERENCE)
		{
			name: "Valid Struct Auto-Dereference",
			input: `
				struct Vec { x: f64 }
				impl Vec { fnc get_x(self: *Self) = f64 { ret self.x; } }
				fnc main() { v: Vec = Vec{ .x = 1.0 }; ptr: *Vec = &v; res: f64 = ptr.get_x(); }
			`,
			expectedError: "",
		},
		{
			name: "Catch Invalid Field Access",
			input: `
				struct Vec { x: f64 }
				fnc main() { v: Vec = Vec{ .x = 1.0 }; res: f64 = v.y; }
			`,
			expectedError: "has no field, method, or variant named 'y'",
		},

		// 4. ENUMS & PATTERN MATCHING
		{
			name: "Valid Tagged Union Exhaustiveness",
			input: `
				enum Opt { None, Some(i32) }
				fnc main() { 
					o: Opt = Opt.Some(5); 
					res: i32 = match o { Opt.Some(v) -> { v }, Opt.None -> { 0 } }; 
				}
			`,
			expectedError: "",
		},
		{
			name: "Catch Incompatible Match Return Types",
			input: `
				enum Opt { None, Some(i32) }
				fnc main() { 
					o: Opt = Opt.Some(5); 
					res: i32 = match o { Opt.Some(v) -> { v }, Opt.None -> { 0.0 } }; 
				}
			`,
			expectedError: "match arms have incompatible return types",
		},

		// 5. HIGHER-ORDER FUNCTIONS (DUCK-TYPING)
		{
			name: "Valid Structural Duck-Typing for Functions",
			input: `
				fnc apply(v: i32, op: fnc(i32)=i32) = i32 { ret op(v); }
				fnc main() { 
					cb: fnc(i32)=i32 = fnc(n: i32)=i32 { ret n * 2; }; 
					res: i32 = apply(5, cb); 
				}
			`,
			expectedError: "",
		},

		// 6. CONTROL FLOW & YIELDING
		{
			name:          "Valid Loop Yielding Slice",
			input:         `fnc main() { arr: [i32; :] = loop(i: i32 = 0...5) { yld i; }; }`,
			expectedError: "",
		},
		{
			name:          "Catch Illegal Break",
			input:         `fnc main() { brk; }`,
			expectedError: "illegal 'brk' statement outside of a loop",
		},

		// 7. DAG HOISTING & SCOPE
		{
			name: "Valid Out-of-Order DAG Execution",
			input: `
				fnc main() { res: i32 = get_val(); }
				fnc get_val() = i32 { ret 42; }
			`,
			expectedError: "",
		},
		{
			name:          "Catch Duplicate Scope Declaration",
			input:         `fnc main() { x: i32 = 1; x: i32 = 2; }`,
			expectedError: "duplicate declaration",
		},

		// 8. CASTING & BUBBLING
		{
			name:          "Valid Cast and Bubble",
			input:         `fnc main() { x: f64 = 10.5; y: i32 = x as i32; z: i32 = y?; }`,
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator, _, err := runSemaPipeline(tt.input)

			if err != nil && tt.expectedError == "" {
				t.Fatalf("\n❌ Unexpected pipeline failure: %v", err)
			}

			hasError := len(evaluator.Errors) > 0
			expectingError := tt.expectedError != ""

			if expectingError && !hasError {
				t.Fatalf("\n❌ Expected error containing '%s', but compilation succeeded.", tt.expectedError)
			}

			if !expectingError && hasError {
				t.Fatalf("\n❌ Expected successful compilation, but got semantic errors:\n  %v", strings.Join(evaluator.Errors, "\n  "))
			}

			if expectingError && hasError {
				errStr := strings.Join(evaluator.Errors, " | ")
				if !strings.Contains(errStr, tt.expectedError) {
					t.Fatalf("\n❌ Expected error to contain:\n  '%s'\nBut got:\n  '%s'", tt.expectedError, errStr)
				}
			}
		})
	}
}
