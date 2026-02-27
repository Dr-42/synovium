package sema

import (
	"fmt"
	"synovium/ast"
)

// Symbol represents a uniquely resolved identifier in the current environment.
type Symbol struct {
	Name      string
	TypeID    TypeID // Points to the UniversalType in the TypePool
	IsMutable bool   // True if declared with ~=

	// Track the AST node for LSP hover text and exact error underlines
	DeclNode ast.Node

	// During Pass 1 (Hoisting), a function or struct might be registered
	// before its exact TypeID is evaluated. We flag it as unresolved.
	IsResolved bool
}

// Scope is a chained lexical environment.
type Scope struct {
	Outer   *Scope
	Symbols map[string]*Symbol
}

// NewScope creates a new inner scope. Pass `nil` to create the Global scope.
func NewScope(outer *Scope) *Scope {
	return &Scope{
		Outer:   outer,
		Symbols: make(map[string]*Symbol),
	}
}

// Define registers a new symbol in the CURRENT scope.
// It enforces Synovium's strict shadowing rules: you cannot declare the same
// variable twice in the exact same block, but you can shadow an outer scope.
func (s *Scope) Define(name string, typeID TypeID, isMut bool, node ast.Node) (*Symbol, error) {
	if _, exists := s.Symbols[name]; exists {
		return nil, fmt.Errorf("duplicate declaration of identifier '%s' in this scope", name)
	}

	sym := &Symbol{
		Name:       name,
		TypeID:     typeID,
		IsMutable:  isMut,
		DeclNode:   node,
		IsResolved: true,
	}
	s.Symbols[name] = sym
	return sym, nil
}

// DefineDeferred is used exclusively during Pass 1 (Hoisting).
// It registers a name so later code knows it exists, but defers the TypeID evaluation.
func (s *Scope) DefineDeferred(name string, node ast.Node) (*Symbol, error) {
	if _, exists := s.Symbols[name]; exists {
		return nil, fmt.Errorf("duplicate declaration of identifier '%s'", name)
	}

	sym := &Symbol{
		Name:       name,
		TypeID:     0,     // Will be patched later
		IsMutable:  false, // Hoisted items (functions/structs) are inherently immutable constants
		DeclNode:   node,
		IsResolved: false,
	}
	s.Symbols[name] = sym
	return sym, nil
}

// Resolve climbs the scope chain to find an identifier.
func (s *Scope) Resolve(name string) (*Symbol, bool) {
	if sym, ok := s.Symbols[name]; ok {
		return sym, true
	}
	// Climb to the parent block
	if s.Outer != nil {
		return s.Outer.Resolve(name)
	}
	// Hit the global ceiling and failed
	return nil, false
}
