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

// DAG manages comptime dependency resolution, cycle detection, and auto-loading.
type DAG struct {
	Nodes         map[string]*GraphNode
	GlobalScope   *Scope
	ParseModule   func(moduleName string) ([]ast.Decl, error) // The Auto-Loader Hook!
	LoadedModules map[string]bool
	ImplCounter   int // Tracks multiple impl blocks for the same struct
}

func NewDAG(globalScope *Scope) *DAG {
	return &DAG{
		Nodes:         make(map[string]*GraphNode),
		GlobalScope:   globalScope,
		LoadedModules: make(map[string]bool),
	}
}

// BuildAndSort hoists declarations, builds the graph, auto-loads files, and prunes dead code.
func (d *DAG) BuildAndSort(program *ast.SourceFile) ([]ast.Decl, error) {
	// 1. Pass 1: Module Auto-Loading & Hoisting
	// We use a dynamically growing queue so imported files can inject new declarations!
	declQueue := append([]ast.Decl{}, program.Declarations...)

	for i := 0; i < len(declQueue); i++ {
		decl := declQueue[i]
		name := d.extractName(decl)

		if name != "" && d.Nodes[name] == nil {
			d.GlobalScope.DefineDeferred(name, decl)
			d.Nodes[name] = &GraphNode{
				Name:          name,
				Decl:          decl,
				Prerequisites: []string{},
				Dependents:    []string{},
				InDegree:      0,
			}
		}

		// THE AUTO-LOADER: Check dependencies and load files if unresolved
		deps := d.extractDependencies(decl)
		for _, req := range deps {
			if !d.LoadedModules[req] {
				d.LoadedModules[req] = true // Mark as attempted
				if d.ParseModule != nil {
					newDecls, err := d.ParseModule(req)
					if err != nil {
						return nil, err
					}
					if len(newDecls) > 0 {
						// Inject the newly parsed file directly into the current DAG queue!
						declQueue = append(declQueue, newDecls...)
					}
				}
			}
		}
	}

	// 2. Pass 2: Build the edges (Prerequisites -> Dependents)
	for name, node := range d.Nodes {
		deps := d.extractDependencies(node.Decl)

		uniquePrereqs := make(map[string]bool)
		for _, req := range deps {
			if req == name {
				continue
			}
			if _, exists := d.Nodes[req]; exists {
				uniquePrereqs[req] = true
			}

			// THE FIX: Only depend on a struct's impl blocks if WE are not an impl block!
			implPrefix := req + "_impl_"
			if !strings.HasPrefix(name, implPrefix) {
				for nName := range d.Nodes {
					if strings.HasPrefix(nName, implPrefix) {
						uniquePrereqs[nName] = true
					}
				}
			}
		}

		for req := range uniquePrereqs {
			node.Prerequisites = append(node.Prerequisites, req)
			node.InDegree++
			d.Nodes[req].Dependents = append(d.Nodes[req].Dependents, name)
		}
	}

	// 3. Kahn's Algorithm for Topological Sort
	var sorted []*GraphNode // Sort the Nodes, not the raw Decls!
	var queue []*GraphNode

	for _, node := range d.Nodes {
		if node.InDegree == 0 {
			queue = append(queue, node)
		}
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		sorted = append(sorted, curr)

		for _, depName := range curr.Dependents {
			depNode := d.Nodes[depName]
			depNode.InDegree--
			if depNode.InDegree == 0 {
				queue = append(queue, depNode)
			}
		}
	}

	if len(sorted) != len(d.Nodes) {
		return nil, fmt.Errorf("cyclic comptime dependency detected involving: %s", d.findCycleNodes())
	}

	// 4. Pass 3: AUTOPRUNING (Dead Code Elimination)
	keep := make(map[string]bool)
	var reachQueue []string

	// Find the Roots
	for name, node := range d.Nodes {
		isRoot := false
		if name == "main" {
			isRoot = true
		} else if fn, ok := node.Decl.(*ast.FunctionDecl); ok && fn.Body == nil {
			isRoot = true // Never prune C FFI headers!
		}

		if isRoot {
			keep[name] = true
			reachQueue = append(reachQueue, name)
		}
	}

	// Backwards Breadth-First Search
	for len(reachQueue) > 0 {
		curr := reachQueue[0]
		reachQueue = reachQueue[1:]

		node := d.Nodes[curr]
		if node == nil {
			continue
		}

		// Keep all prerequisites required by this node
		for _, req := range node.Prerequisites {
			if !keep[req] {
				keep[req] = true
				reachQueue = append(reachQueue, req)
			}
		}

		// If we keep a struct, we MUST automatically keep all of its impl blocks!
		implPrefix := curr + "_impl_"
		for nName := range d.Nodes {
			if strings.HasPrefix(nName, implPrefix) && !keep[nName] {
				keep[nName] = true
				reachQueue = append(reachQueue, nName)
			}
		}
	}

	// Filter the final AST payload
	var pruned []ast.Decl
	for _, node := range sorted {
		if keep[node.Name] {
			pruned = append(pruned, node.Decl)
		}
	}

	return pruned, nil
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
		// Generate a unique ID so multiple impl blocks for the same struct don't overwrite each other!
		d.ImplCounter++
		return fmt.Sprintf("%s_impl_%d", v.Target.Value, d.ImplCounter)
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
		case *ast.DeferStmt: // Replaced YieldStmt
			if v == nil {
				return
			}
			visit(v.Body)
		case *ast.BreakStmt: // Now takes a value!
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
			visit(v.ElseBody)
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
		case *ast.BubbleExpr:
			if v == nil {
				return
			}
			visit(v.Left)
		case *ast.CastExpr:
			if v == nil {
				return
			}
			visit(v.Left)
			visit(v.Type)
		case *ast.IndexExpr:
			if v == nil {
				return
			}
			visit(v.Left)
			visit(v.Index)
		case *ast.FieldAccessExpr:
			if v == nil {
				return
			}
			visit(v.Left)
		case *ast.ArrayInitExpr:
			if v == nil {
				return
			}
			for _, el := range v.Elements {
				visit(el)
			}
			if v.Count != nil {
				visit(v.Count)
			}
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
