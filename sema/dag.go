package sema

import (
	"fmt"
	"strings"
	"synovium/ast"
)

// GraphNode tracks dependencies for topological sorting.
type GraphNode struct {
	Name          string
	Decl          ast.Decl
	Prerequisites []string // Names of global symbols this node needs to be evaluated first
	Dependents    []string // Names of global symbols that rely on this node
	InDegree      int      // Number of unresolved prerequisites
}

// DAG manages comptime dependency resolution and cycle detection.
type DAG struct {
	Nodes       map[string]*GraphNode
	GlobalScope *Scope
}

func NewDAG(globalScope *Scope) *DAG {
	return &DAG{
		Nodes:       make(map[string]*GraphNode),
		GlobalScope: globalScope,
	}
}

// BuildAndSort hoists declarations, builds the graph, checks for cycles,
// and returns the topologically sorted execution order.
func (d *DAG) BuildAndSort(program *ast.SourceFile) ([]ast.Decl, error) {
	// 1. Pass 1: Hoist declarations and initialize nodes
	for _, decl := range program.Declarations {
		name := d.extractName(decl)
		if name == "" {
			continue // Unnamed or illegal decls (caught by parser)
		}

		// Register in global scope as a Deferred symbol so scopes can find it
		_, err := d.GlobalScope.DefineDeferred(name, decl)
		if err != nil {
			return nil, err
		}

		d.Nodes[name] = &GraphNode{
			Name:          name,
			Decl:          decl,
			Prerequisites: []string{},
			Dependents:    []string{},
			InDegree:      0,
		}
	}

	// 2. Pass 2: Build the edges (Prerequisites -> Dependents)
	for name, node := range d.Nodes {
		deps := d.extractDependencies(node.Decl)

		uniquePrereqs := make(map[string]bool)
		for _, req := range deps {
			// Ignore self-recursion; a function calling itself is not a strict comptime cycle
			if req == name {
				continue
			}
			// Only track dependencies that map to other top-level global nodes
			if _, exists := d.Nodes[req]; exists {
				uniquePrereqs[req] = true
			}
		}

		for req := range uniquePrereqs {
			node.Prerequisites = append(node.Prerequisites, req)
			node.InDegree++
			d.Nodes[req].Dependents = append(d.Nodes[req].Dependents, name)
		}
	}

	// 3. Kahn's Algorithm for Topological Sort & Cycle Detection
	var sorted []ast.Decl
	var queue []*GraphNode

	// Enqueue all axioms (nodes with no prerequisites)
	for _, node := range d.Nodes {
		if node.InDegree == 0 {
			queue = append(queue, node)
		}
	}

	for len(queue) > 0 {
		// Pop the next available node
		curr := queue[0]
		queue = queue[1:]

		sorted = append(sorted, curr.Decl)

		// Resolve this prerequisite for all dependents
		for _, depName := range curr.Dependents {
			depNode := d.Nodes[depName]
			depNode.InDegree--
			if depNode.InDegree == 0 {
				queue = append(queue, depNode)
			}
		}
	}

	// 4. Cycle Detection Check
	if len(sorted) != len(d.Nodes) {
		return nil, fmt.Errorf("cyclic comptime dependency detected involving: %s", d.findCycleNodes())
	}

	return sorted, nil
}

// extractName grabs the string identifier of a top-level declaration.
func (d *DAG) extractName(decl ast.Decl) string {
	switch v := decl.(type) {
	case *ast.VariableDecl:
		return v.Name.Value
	case *ast.FunctionDecl:
		if v.Name != nil {
			return v.Name.Value
		}
	case *ast.StructDecl:
		return v.Name.Value
	case *ast.EnumDecl:
		return v.Name.Value
	case *ast.ImplDecl:
		// Impl blocks don't create new names, they attach to existing structs
		// For ordering, we attach them via the Target name
		return v.Target.Value + "_impl"
	}
	return ""
}

// extractDependencies recursively walks the AST of a declaration to find all Identifiers.
func (d *DAG) extractDependencies(node ast.Node) []string {
	var deps []string

	var visit func(n ast.Node)
	visit = func(n ast.Node) {
		if n == nil {
			return
		}

		switch v := n.(type) {
		case *ast.Identifier:
			if v == nil {
				return
			}
			deps = append(deps, v.Value)
		case *ast.NamedType:
			if v == nil {
				return
			}
			// "std.math.Vec3" maps to a dependency on "std"
			baseName := strings.Split(v.Name, ".")[0]
			deps = append(deps, baseName)

		// Unpack Declarations
		case *ast.VariableDecl:
			if v == nil {
				return
			}
			visit(v.Type)
			visit(v.Value)
		case *ast.FunctionDecl:
			if v == nil {
				return
			}
			for _, p := range v.Parameters {
				visit(p.Type)
			}
			visit(v.ReturnType)
			visit(v.Body)
		case *ast.StructDecl:
			if v == nil {
				return
			}
			for _, f := range v.Fields {
				visit(f.Type)
			}
		case *ast.EnumDecl:
			if v == nil {
				return
			}
			for _, variant := range v.Variants {
				for _, t := range variant.Types {
					visit(t)
				}
			}
		case *ast.ImplDecl:
			if v == nil {
				return
			}
			visit(v.Target)
			for _, m := range v.Methods {
				visit(m)
			}

		// Unpack Expressions
		case *ast.Block:
			if v == nil {
				return
			}
			for _, s := range v.Statements {
				visit(s)
			}
			visit(v.Value)
		case *ast.ExprStmt:
			if v == nil {
				return
			}
			visit(v.Value)
		case *ast.ReturnStmt:
			if v == nil {
				return
			}
			visit(v.Value)
		case *ast.YieldStmt:
			if v == nil {
				return
			}
			visit(v.Value)
		case *ast.InfixExpr:
			if v == nil {
				return
			}
			visit(v.Left)
			visit(v.Right)
		case *ast.PrefixExpr:
			if v == nil {
				return
			}
			visit(v.Right)
		case *ast.CallExpr:
			if v == nil {
				return
			}
			visit(v.Function)
			for _, a := range v.Arguments {
				visit(a)
			}
		case *ast.StructInitExpr:
			if v == nil {
				return
			}
			visit(v.Name)
			for _, f := range v.Fields {
				visit(f.Value)
			}
		case *ast.IfExpr:
			if v == nil {
				return
			}
			visit(v.Condition)
			visit(v.Body)
			for _, c := range v.ElifConds {
				visit(c)
			}
			for _, b := range v.ElifBodies {
				visit(b)
			}
			visit(v.ElseBody) // The typed-nil trap is now neutralized!
		case *ast.LoopExpr:
			if v == nil {
				return
			}
			visit(v.Condition)
			visit(v.Body)
		case *ast.MatchExpr:
			if v == nil {
				return
			}
			visit(v.Value)
			for _, a := range v.Arms {
				visit(a.Pattern)
				visit(a.Body)
			}
		case *ast.ArrayType:
			if v == nil {
				return
			}
			visit(v.Base)
			visit(v.Size)
		case *ast.PointerType:
			if v == nil {
				return
			}
			visit(v.Base)
		case *ast.ReferenceType:
			if v == nil {
				return
			}
			visit(v.Base)
		case *ast.FunctionType:
			if v == nil {
				return
			}
			for _, param := range v.Parameters {
				visit(param)
			}
			visit(v.ReturnType)
		}
	}

	visit(node)
	return deps
}

// findCycleNodes isolates which nodes failed to resolve for error reporting.
func (d *DAG) findCycleNodes() string {
	var stuck []string
	for name, node := range d.Nodes {
		if node.InDegree > 0 {
			stuck = append(stuck, name)
		}
	}
	return strings.Join(stuck, ", ")
}
