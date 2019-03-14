// Copyright 2018 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package ast declares the types used to represent syntax trees for CUE
// packages.
package ast // import "cuelang.org/go/cue/ast"

import (
	"strconv"
	"strings"

	"cuelang.org/go/cue/token"
)

// ----------------------------------------------------------------------------
// Interfaces
//
// There are two main classes of nodes: expressions, clauses, and declaration
// nodes. The node names usually match the corresponding CUE spec production
// names to which they correspond. The node fields correspond to the individual
// parts of the respective productions.
//
// All nodes contain position information marking the beginning of the
// corresponding source text segment; it is accessible via the Pos accessor
// method. Nodes may contain additional position info for language constructs
// where comments may be found between parts of the construct (typically any
// larger, parenthesized subpart). That position information is needed to
// properly position comments when printing the construct.

// A Node represents any node in the abstract syntax tree.
type Node interface {
	Pos() token.Pos // position of first character belonging to the node
	End() token.Pos // position of first character immediately after the node

	// TODO: SetPos(p token.RelPos)

	Comments() []*CommentGroup
	AddComment(*CommentGroup)
}

// An Expr is implemented by all expression nodes.
type Expr interface {
	Node
	exprNode()
}

func (*BadExpr) exprNode()       {}
func (*Ident) exprNode()         {}
func (*BasicLit) exprNode()      {}
func (*Interpolation) exprNode() {}
func (*StructLit) exprNode()     {}
func (*ListLit) exprNode()       {}

// func (*StructComprehension) exprNode() {}
func (*ListComprehension) exprNode() {}
func (*ParenExpr) exprNode()         {}
func (*SelectorExpr) exprNode()      {}
func (*IndexExpr) exprNode()         {}
func (*SliceExpr) exprNode()         {}
func (*CallExpr) exprNode()          {}
func (*UnaryExpr) exprNode()         {}
func (*BinaryExpr) exprNode()        {}
func (*BottomLit) exprNode()         {}

// A Decl node is implemented by all declarations.
type Decl interface {
	Node
	declNode()
}

func (*Field) declNode()             {}
func (*ComprehensionDecl) declNode() {}
func (*ImportDecl) declNode()        {}
func (*BadDecl) declNode()           {}
func (*EmitDecl) declNode()          {}
func (*Alias) declNode()             {}

// A Label is any prduction that can be used as a LHS label.
type Label interface {
	Node
	labelName() (name string, isIdent bool)
}

func (n *Ident) labelName() (string, bool) {
	return n.Name, true
}

func (n *BasicLit) labelName() (string, bool) {
	switch n.Kind {
	case token.STRING:
		// Use strconv to only allow double-quoted, single-line strings.
		if str, err := strconv.Unquote(n.Value); err == nil {
			return str, true
		}
	case token.NULL, token.TRUE, token.FALSE:
		return n.Value, true

		// TODO: allow numbers to be fields?
	}
	return "", false
}

func (n *TemplateLabel) labelName() (string, bool) {
	return n.Ident.Name, false
}

func (n *Interpolation) labelName() (string, bool) {
	return "", false
}

// LabelName reports the name of a label, if known, and whether it is valid.
func LabelName(x Label) (name string, ok bool) {
	return x.labelName()
}

// Clause nodes are part of comprehensions.
type Clause interface {
	Node
	clauseNode()
}

func (x *ForClause) clauseNode() {}
func (x *IfClause) clauseNode()  {}
func (x *Alias) clauseNode()     {}

// Comments

type comments struct {
	groups *[]*CommentGroup
}

func (c *comments) Comments() []*CommentGroup {
	if c.groups == nil {
		return []*CommentGroup{}
	}
	return *c.groups
}

// // AddComment adds the given comments to the fields.
// // If line is true the comment is inserted at the preceding token.

func (c *comments) AddComment(cg *CommentGroup) {
	if cg == nil {
		return
	}
	if c.groups == nil {
		a := []*CommentGroup{cg}
		c.groups = &a
		return
	}
	*c.groups = append(*c.groups, cg)
}

// A Comment node represents a single //-style or /*-style comment.
type Comment struct {
	Slash token.Pos // position of "/" starting the comment
	Text  string    // comment text (excluding '\n' for //-style comments)
}

func (g *Comment) Comments() []*CommentGroup { return nil }
func (g *Comment) AddComment(*CommentGroup)  {}

func (c *Comment) Pos() token.Pos { return c.Slash }
func (c *Comment) End() token.Pos { return c.Slash.Add(len(c.Text)) }

// A CommentGroup represents a sequence of comments
// with no other tokens and no empty lines between.
type CommentGroup struct {
	// TODO: remove and use the token position of the first commment.
	Doc  bool
	Line bool // true if it is on the same line as the node's end pos.

	// Position indicates where a comment should be attached if a node has
	// multiple tokens. 0 means before the first token, 1 means before the
	// second, etc. For instance, for a field, the positions are:
	//    <0> Label <1> ":" <2> Expr <3> "," <4>
	Position int8
	List     []*Comment // len(List) > 0
}

func (g *CommentGroup) Pos() token.Pos { return g.List[0].Pos() }
func (g *CommentGroup) End() token.Pos { return g.List[len(g.List)-1].End() }

func (g *CommentGroup) Comments() []*CommentGroup { return nil }
func (g *CommentGroup) AddComment(*CommentGroup)  {}

func isWhitespace(ch byte) bool { return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' }

func stripTrailingWhitespace(s string) string {
	i := len(s)
	for i > 0 && isWhitespace(s[i-1]) {
		i--
	}
	return s[0:i]
}

// Text returns the text of the comment.
// Comment markers (//, /*, and */), the first space of a line comment, and
// leading and trailing empty lines are removed. Multiple empty lines are
// reduced to one, and trailing space on lines is trimmed. Unless the result
// is empty, it is newline-terminated.
func (g *CommentGroup) Text() string {
	if g == nil {
		return ""
	}
	comments := make([]string, len(g.List))
	for i, c := range g.List {
		comments[i] = c.Text
	}

	lines := make([]string, 0, 10) // most comments are less than 10 lines
	for _, c := range comments {
		// Remove comment markers.
		// The parser has given us exactly the comment text.
		switch c[1] {
		case '/':
			//-style comment (no newline at the end)
			c = c[2:]
			// strip first space - required for Example tests
			if len(c) > 0 && c[0] == ' ' {
				c = c[1:]
			}
		case '*':
			/*-style comment */
			c = c[2 : len(c)-2]
		}

		// Split on newlines.
		cl := strings.Split(c, "\n")

		// Walk lines, stripping trailing white space and adding to list.
		for _, l := range cl {
			lines = append(lines, stripTrailingWhitespace(l))
		}
	}

	// Remove leading blank lines; convert runs of
	// interior blank lines to a single blank line.
	n := 0
	for _, line := range lines {
		if line != "" || n > 0 && lines[n-1] != "" {
			lines[n] = line
			n++
		}
	}
	lines = lines[0:n]

	// Add final "" entry to get trailing newline from Join.
	if n > 0 && lines[n-1] != "" {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// An Attribute provides meta data about a field.
type Attribute struct {
	comments
	At   token.Pos
	Text string // must be a valid attribute format.
}

func (a *Attribute) Pos() token.Pos { return a.At }
func (a *Attribute) End() token.Pos { return a.At.Add(len(a.Text)) }

// A Field represents a field declaration in a struct.
type Field struct {
	comments
	Label Label // must have at least one element.

	// No colon: Value must be an StructLit with one field.
	Colon token.Pos
	Value Expr // the value associated with this field.

	Attrs []*Attribute
}

func (d *Field) Pos() token.Pos { return d.Label.Pos() }
func (d *Field) End() token.Pos {
	if len(d.Attrs) > 0 {
		return d.Attrs[len(d.Attrs)-1].End()
	}
	return d.Value.End()
}

// An Alias binds another field to the alias name in the current struct.
type Alias struct {
	comments
	Ident *Ident    // field name, always an Ident
	Equal token.Pos // position of "="
	Expr  Expr      // An Ident or SelectorExpr
}

func (a *Alias) Pos() token.Pos { return a.Ident.Pos() }
func (a *Alias) End() token.Pos { return a.Expr.End() }

// A ComprehensionDecl node represents a field comprehension.
type ComprehensionDecl struct {
	comments
	Field   *Field
	Select  token.Pos
	Clauses []Clause
}

func (x *ComprehensionDecl) Pos() token.Pos { return x.Field.Pos() }
func (x *ComprehensionDecl) End() token.Pos {
	if len(x.Clauses) > 0 {
		return x.Clauses[len(x.Clauses)-1].End()
	}
	return x.Select
}

// ----------------------------------------------------------------------------
// Expressions and types
//
// An expression is represented by a tree consisting of one
// or more of the following concrete expression nodes.

// A BadExpr node is a placeholder for expressions containing
// syntax errors for which no correct expression nodes can be
// created. This is different from an ErrorExpr which represents
// an explicitly marked error in the source.
type BadExpr struct {
	comments
	From, To token.Pos // position range of bad expression
}

// A BottomLit indicates an error.
type BottomLit struct {
	comments
	Bottom token.Pos
}

// An Ident node represents an left-hand side identifier.
type Ident struct {
	comments
	NamePos token.Pos // identifier position

	// This LHS path element may be an identifier. Possible forms:
	//  foo:    a normal identifier
	//  "foo":  JSON compatible
	//  <foo>:  a template shorthand
	Name string

	Scope Node // scope in which node was found or nil if referring directly
	Node  Node
}

// A TemplateLabel represents a field template declaration in a struct.
type TemplateLabel struct {
	comments
	Langle token.Pos
	Ident  *Ident
	Rangle token.Pos
}

// A BasicLit node represents a literal of basic type.
type BasicLit struct {
	comments
	ValuePos token.Pos   // literal position
	Kind     token.Token // INT, FLOAT, DURATION, or STRING
	Value    string      // literal string; e.g. 42, 0x7f, 3.14, 1_234_567, 1e-9, 2.4i, 'a', '\x7f', "foo", or '\m\n\o'
}

// A Interpolation node represents a string or bytes interpolation.
type Interpolation struct { // TODO: rename to TemplateLit
	comments
	Elts []Expr // interleaving of strings and expressions.
}

// A StructLit node represents a literal struct.
type StructLit struct {
	comments
	Lbrace token.Pos // position of "{"
	Elts   []Decl    // list of elements; or nil
	Rbrace token.Pos // position of "}"
}

// A ListLit node represents a literal list.
type ListLit struct {
	comments
	Lbrack   token.Pos // position of "["
	Elts     []Expr    // list of composite elements; or nil
	Ellipsis token.Pos // open list if set
	Type     Expr      // type for the remaining elements
	Rbrack   token.Pos // position of "]"
}

// A ListComprehension node represents as list comprehension.
type ListComprehension struct {
	comments
	Lbrack  token.Pos // position of "["
	Expr    Expr
	Clauses []Clause  // Feed or Guard (TODO let)
	Rbrack  token.Pos // position of "]"
}

// A ForClause node represents a for clause in a comprehension.
type ForClause struct {
	comments
	For    token.Pos
	Key    *Ident // allow pattern matching?
	Colon  token.Pos
	Value  *Ident // allow pattern matching?
	In     token.Pos
	Source Expr
}

// A IfClause node represents an if guard clause in a comprehension.
type IfClause struct {
	comments
	If        token.Pos
	Condition Expr
}

// A ParenExpr node represents a parenthesized expression.
type ParenExpr struct {
	comments
	Lparen token.Pos // position of "("
	X      Expr      // parenthesized expression
	Rparen token.Pos // position of ")"
}

// A SelectorExpr node represents an expression followed by a selector.
type SelectorExpr struct {
	comments
	X   Expr   // expression
	Sel *Ident // field selector
}

// An IndexExpr node represents an expression followed by an index.
type IndexExpr struct {
	comments
	X      Expr      // expression
	Lbrack token.Pos // position of "["
	Index  Expr      // index expression
	Rbrack token.Pos // position of "]"
}

// An SliceExpr node represents an expression followed by slice indices.
type SliceExpr struct {
	comments
	X      Expr      // expression
	Lbrack token.Pos // position of "["
	Low    Expr      // begin of slice range; or nil
	High   Expr      // end of slice range; or nil
	Rbrack token.Pos // position of "]"
}

// A CallExpr node represents an expression followed by an argument list.
type CallExpr struct {
	comments
	Fun    Expr      // function expression
	Lparen token.Pos // position of "("
	Args   []Expr    // function arguments; or nil
	Rparen token.Pos // position of ")"
}

// A UnaryExpr node represents a unary expression.
type UnaryExpr struct {
	comments
	OpPos token.Pos   // position of Op
	Op    token.Token // operator
	X     Expr        // operand
}

// A BinaryExpr node represents a binary expression.
type BinaryExpr struct {
	comments
	X     Expr        // left operand
	OpPos token.Pos   // position of Op
	Op    token.Token // operator
	Y     Expr        // right operand
}

// token.Pos and End implementations for expression/type nodes.

func (x *BadExpr) Pos() token.Pos       { return x.From }
func (x *Ident) Pos() token.Pos         { return x.NamePos }
func (x *TemplateLabel) Pos() token.Pos { return x.Langle }
func (x *BasicLit) Pos() token.Pos      { return x.ValuePos }
func (x *Interpolation) Pos() token.Pos { return x.Elts[0].Pos() }
func (x *StructLit) Pos() token.Pos {
	if x.Lbrace == token.NoPos && len(x.Elts) > 0 {
		return x.Elts[0].Pos()
	}
	return x.Lbrace
}

func (x *ListLit) Pos() token.Pos           { return x.Lbrack }
func (x *ListComprehension) Pos() token.Pos { return x.Lbrack }
func (x *ForClause) Pos() token.Pos         { return x.For }
func (x *IfClause) Pos() token.Pos          { return x.If }
func (x *ParenExpr) Pos() token.Pos         { return x.Lparen }
func (x *SelectorExpr) Pos() token.Pos      { return x.X.Pos() }
func (x *IndexExpr) Pos() token.Pos         { return x.X.Pos() }
func (x *SliceExpr) Pos() token.Pos         { return x.X.Pos() }
func (x *CallExpr) Pos() token.Pos          { return x.Fun.Pos() }
func (x *UnaryExpr) Pos() token.Pos         { return x.OpPos }
func (x *BinaryExpr) Pos() token.Pos        { return x.X.Pos() }
func (x *BottomLit) Pos() token.Pos         { return x.Bottom }

func (x *BadExpr) End() token.Pos { return x.To }
func (x *Ident) End() token.Pos {
	return x.NamePos.Add(len(x.Name))
}
func (x *TemplateLabel) End() token.Pos { return x.Rangle }
func (x *BasicLit) End() token.Pos      { return token.Pos(int(x.ValuePos) + len(x.Value)) }
func (x *Interpolation) End() token.Pos { return x.Elts[len(x.Elts)-1].Pos() }
func (x *StructLit) End() token.Pos {
	if x.Rbrace == token.NoPos && len(x.Elts) > 0 {
		return x.Elts[len(x.Elts)-1].Pos()
	}
	return x.Rbrace.Add(1)
}
func (x *ListLit) End() token.Pos           { return x.Rbrack.Add(1) }
func (x *ListComprehension) End() token.Pos { return x.Rbrack }
func (x *ForClause) End() token.Pos         { return x.Source.End() }
func (x *IfClause) End() token.Pos          { return x.Condition.End() }
func (x *ParenExpr) End() token.Pos         { return x.Rparen.Add(1) }
func (x *SelectorExpr) End() token.Pos      { return x.Sel.End() }
func (x *IndexExpr) End() token.Pos         { return x.Rbrack.Add(1) }
func (x *SliceExpr) End() token.Pos         { return x.Rbrack.Add(1) }
func (x *CallExpr) End() token.Pos          { return x.Rparen.Add(1) }
func (x *UnaryExpr) End() token.Pos         { return x.X.End() }
func (x *BinaryExpr) End() token.Pos        { return x.Y.End() }
func (x *BottomLit) End() token.Pos         { return x.Bottom.Add(1) }

// ----------------------------------------------------------------------------
// Convenience functions for Idents

// NewIdent creates a new Ident without position.
// Useful for ASTs generated by code other than the Go
func NewIdent(name string) *Ident {
	return &Ident{comments{}, token.NoPos, name, nil, nil}
}

func (id *Ident) String() string {
	if id != nil {
		return id.Name
	}
	return "<nil>"
}

// ----------------------------------------------------------------------------
// Declarations

// An ImportSpec node represents a single package import.
type ImportSpec struct {
	comments
	Name   *Ident    // local package name (including "."); or nil
	Path   *BasicLit // import path
	EndPos token.Pos // end of spec (overrides Path.Pos if nonzero)
}

// Pos and End implementations for spec nodes.

func (s *ImportSpec) Pos() token.Pos {
	if s.Name != nil {
		return s.Name.Pos()
	}
	return s.Path.Pos()
}

// func (s *AliasSpec) Pos() token.Pos { return s.Name.Pos() }
// func (s *ValueSpec) Pos() token.Pos { return s.Names[0].Pos() }
// func (s *TypeSpec) Pos() token.Pos  { return s.Name.Pos() }

func (s *ImportSpec) End() token.Pos {
	if s.EndPos != 0 {
		return s.EndPos
	}
	return s.Path.End()
}

// specNode() ensures that only spec nodes can be assigned to a Spec.
func (*ImportSpec) specNode() {}

// A declaration is represented by one of the following declaration nodes.
type (
	// A BadDecl node is a placeholder for declarations containing
	// syntax errors for which no correct declaration nodes can be
	// created.
	BadDecl struct {
		comments
		From, To token.Pos // position range of bad declaration
	}

	// A ImportDecl node represents a series of import declarations. A valid
	// Lparen position (Lparen.Line > 0) indicates a parenthesized declaration.
	ImportDecl struct {
		comments
		Import token.Pos
		Lparen token.Pos // position of '(', if any
		Specs  []*ImportSpec
		Rparen token.Pos // position of ')', if any
	}

	// An EmitDecl node represents a single expression used as a declaration.
	// The expressions in this declaration is what will be emitted as
	// configuration output.
	//
	// An EmitDecl may only appear at the top level.
	EmitDecl struct {
		comments
		Expr Expr
	}
)

// Pos and End implementations for declaration nodes.

func (d *BadDecl) Pos() token.Pos    { return d.From }
func (d *ImportDecl) Pos() token.Pos { return d.Import }
func (d *EmitDecl) Pos() token.Pos   { return d.Expr.Pos() }

func (d *BadDecl) End() token.Pos { return d.To }
func (d *ImportDecl) End() token.Pos {
	if d.Rparen.IsValid() {
		return d.Rparen.Add(1)
	}
	return d.Specs[0].End()
}
func (d *EmitDecl) End() token.Pos { return d.Expr.End() }

// ----------------------------------------------------------------------------
// Files and packages

// A File node represents a Go source file.
//
// The Comments list contains all comments in the source file in order of
// appearance, including the comments that are pointed to from other nodes
// via Doc and Comment fields.
type File struct {
	Filename string
	comments
	Package token.Pos // position of "package" pseudo-keyword
	Name    *Ident    // package names
	// TODO: Change Expr to Decl?
	Imports    []*ImportSpec // imports in this file
	Decls      []Decl        // top-level declarations; or nil
	Unresolved []*Ident      // unresolved identifiers in this file
}

func (f *File) Pos() token.Pos {
	if f.Package != token.NoPos {
		return f.Package
	}
	if len(f.Decls) > 0 {
		return f.Decls[0].Pos()
	}
	return token.NoPos
}
func (f *File) End() token.Pos {
	if n := len(f.Decls); n > 0 {
		return f.Decls[n-1].End()
	}
	if f.Name != nil {
		return f.Name.End()
	}
	return token.NoPos
}
