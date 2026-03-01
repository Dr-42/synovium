package ast

import "fmt"

// CloneNode deeply duplicates an AST node and asserts the correct interface types.
// This ensures that multiple instances of the same generic function
// do not overwrite each other's TAST side-table annotations!
func CloneNode(node Node) Node {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	// -- Literals & Identifiers --
	case *Identifier:
		return &Identifier{Token: n.Token, Value: n.Value}
	case *IntLiteral:
		return &IntLiteral{Token: n.Token, Value: n.Value}
	case *FloatLiteral:
		return &FloatLiteral{Token: n.Token, Value: n.Value}
	case *StringLiteral:
		return &StringLiteral{Token: n.Token, Value: n.Value}
	case *CharLiteral:
		return &CharLiteral{Token: n.Token, Value: n.Value}
	case *BoolLiteral:
		return &BoolLiteral{Token: n.Token, Value: n.Value}

	// -- Declarations --
	case *VariableDecl:
		return &VariableDecl{
			Token:    n.Token,
			Name:     CloneNode(n.Name).(*Identifier),
			Type:     CloneNode(n.Type).(Type),
			Operator: n.Operator,
			Value:    CloneNode(n.Value).(Expr),
		}
	case *Parameter:
		return &Parameter{
			Name: CloneNode(n.Name).(*Identifier),
			Type: CloneNode(n.Type).(Type),
		}
	case *FunctionDecl:
		var clonedName *Identifier
		if n.Name != nil {
			clonedName = CloneNode(n.Name).(*Identifier)
		}
		params := make([]*Parameter, len(n.Parameters))
		for i, p := range n.Parameters {
			params[i] = CloneNode(p).(*Parameter)
		}
		var retType Type
		if n.ReturnType != nil {
			retType = CloneNode(n.ReturnType).(Type)
		}
		var body *Block
		if n.Body != nil {
			body = CloneNode(n.Body).(*Block)
		}
		return &FunctionDecl{
			Token:      n.Token,
			Name:       clonedName,
			Parameters: params,
			ReturnType: retType,
			Body:       body,
			IsVariadic: n.IsVariadic,
		}

	// -- Statements --
	case *Block:
		stmts := make([]Stmt, len(n.Statements))
		for i, s := range n.Statements {
			stmts[i] = CloneNode(s).(Stmt)
		}
		var val Expr
		if n.Value != nil {
			val = CloneNode(n.Value).(Expr)
		}
		return &Block{Token: n.Token, Statements: stmts, Value: val}
	case *ExprStmt:
		return &ExprStmt{Token: n.Token, Value: CloneNode(n.Value).(Expr)}
	case *ReturnStmt:
		var val Expr
		if n.Value != nil {
			val = CloneNode(n.Value).(Expr)
		}
		return &ReturnStmt{Token: n.Token, Value: val}
	case *YieldStmt:
		var val Expr
		if n.Value != nil {
			val = CloneNode(n.Value).(Expr)
		}
		return &YieldStmt{Token: n.Token, Value: val}
	case *BreakStmt:
		return &BreakStmt{Token: n.Token}

	// -- Expressions --
	case *InfixExpr:
		return &InfixExpr{Token: n.Token, Left: CloneNode(n.Left).(Expr), Operator: n.Operator, Right: CloneNode(n.Right).(Expr)}
	case *PrefixExpr:
		return &PrefixExpr{Token: n.Token, Operator: n.Operator, Right: CloneNode(n.Right).(Expr)}
	case *CallExpr:
		args := make([]Expr, len(n.Arguments))
		for i, a := range n.Arguments {
			args[i] = CloneNode(a).(Expr)
		}
		return &CallExpr{Token: n.Token, Function: CloneNode(n.Function).(Expr), Arguments: args}
	case *FieldAccessExpr:
		return &FieldAccessExpr{Token: n.Token, Left: CloneNode(n.Left).(Expr), Field: CloneNode(n.Field).(*Identifier)}
	case *IndexExpr:
		return &IndexExpr{Token: n.Token, Left: CloneNode(n.Left).(Expr), Index: CloneNode(n.Index).(Expr)}
	case *IfExpr:
		elifConds := make([]Expr, len(n.ElifConds))
		for i, c := range n.ElifConds {
			elifConds[i] = CloneNode(c).(Expr)
		}
		elifBodies := make([]*Block, len(n.ElifBodies))
		for i, b := range n.ElifBodies {
			elifBodies[i] = CloneNode(b).(*Block)
		}
		var elseBody *Block
		if n.ElseBody != nil {
			elseBody = CloneNode(n.ElseBody).(*Block)
		}
		return &IfExpr{
			Token:      n.Token,
			Condition:  CloneNode(n.Condition).(Expr),
			Body:       CloneNode(n.Body).(*Block),
			ElifConds:  elifConds,
			ElifBodies: elifBodies,
			ElseBody:   elseBody,
		}
	case *LoopExpr:
		var cond Node
		if n.Condition != nil {
			cond = CloneNode(n.Condition)
		}
		return &LoopExpr{Token: n.Token, Condition: cond, Body: CloneNode(n.Body).(*Block)}
	case *CastExpr:
		return &CastExpr{Token: n.Token, Left: CloneNode(n.Left).(Expr), Type: CloneNode(n.Type).(Type)}
	case *BubbleExpr:
		return &BubbleExpr{Token: n.Token, Left: CloneNode(n.Left).(Expr)}
	case *StructInitExpr:
		fields := make([]*StructInitField, len(n.Fields))
		for i, f := range n.Fields {
			fields[i] = CloneNode(f).(*StructInitField)
		}
		return &StructInitExpr{Token: n.Token, Name: CloneNode(n.Name).(*Identifier), Fields: fields}
	case *StructInitField:
		return &StructInitField{Token: n.Token, Name: CloneNode(n.Name).(*Identifier), Value: CloneNode(n.Value).(Expr)}
	case *MatchExpr:
		arms := make([]*MatchArm, len(n.Arms))
		for i, a := range n.Arms {
			arms[i] = CloneNode(a).(*MatchArm)
		}
		return &MatchExpr{Token: n.Token, Value: CloneNode(n.Value).(Expr), Arms: arms}
	case *MatchArm:
		params := make([]*Identifier, len(n.Params))
		for i, p := range n.Params {
			params[i] = CloneNode(p).(*Identifier)
		}
		return &MatchArm{Token: n.Token, Pattern: CloneNode(n.Pattern).(*Identifier), Params: params, Body: CloneNode(n.Body).(*Block)}

	// -- Types --
	case *NamedType:
		return &NamedType{Token: n.Token, Name: n.Name}
	case *PointerType:
		return &PointerType{Token: n.Token, Base: CloneNode(n.Base).(Type)}
	case *ReferenceType:
		return &ReferenceType{Token: n.Token, Base: CloneNode(n.Base).(Type)}
	case *ArrayType:
		var size Expr
		if n.Size != nil {
			size = CloneNode(n.Size).(Expr)
		}
		return &ArrayType{Token: n.Token, Base: CloneNode(n.Base).(Type), Size: size, IsSlice: n.IsSlice}
	case *FunctionType:
		params := make([]Type, len(n.Parameters))
		for i, p := range n.Parameters {
			params[i] = CloneNode(p).(Type)
		}
		var retType Type
		if n.ReturnType != nil {
			retType = CloneNode(n.ReturnType).(Type)
		}
		return &FunctionType{
			Token:      n.Token,
			Parameters: params,
			ReturnType: retType,
			IsVariadic: n.IsVariadic,
		}
	}

	panic(fmt.Sprintf("CloneNode: unhandled node type %T", node))
}
