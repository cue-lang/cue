package yaml

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// TODO(mvdan): we should sanity check that the decoder always produces valid CUE,
// as it is possible to construct a cue/ast syntax tree with invalid literals
// or with expressions that will always error, such as `float & 123`.
//
// One option would be to do this as part of the tests; a more general approach
// may be fuzzing, which would find more bugs and work for any decoder,
// although it may be slow as we need to involve the evaluator.

type Decoder struct {
	yamlDecoder yaml.Decoder
	tokFile     *token.File
	tokLines    []int

	pendingHeadComments []*ast.Comment

	// extractingAliases ensures we don't loop forever when expanding YAML anchors.
	extractingAliases map[*yaml.Node]bool

	// lastPos is the last YAML node position that we decoded,
	// used for working out relative positions such as token.NewSection.
	lastPos token.Position

	// forceNewline ensures that the next position will be on a new line.
	forceNewline bool
}

// TODO(mvdan): this can be io.Reader really, except that token.Pos is offset-based,
// so the only way to really have true Offset+Line+Col numbers is to know
// the size of the entire YAML node upfront.
// With json we can use RawMessage to know the size of the input
// before we extract into ast.Expr, but unfortunately, yaml.Node has no size.

func NewDecoder(filename string, b []byte) *Decoder {
	// Note that yaml.v3 can insert a null node just past the end of the input
	// in some edge cases, so we pretend that there's an extra newline
	// so that we don't panic when handling such a position.
	tokFile := token.NewFile(filename, 0, len(b)+1)
	tokFile.SetLinesForContent(b)
	return &Decoder{
		tokFile:     tokFile,
		tokLines:    append(tokFile.Lines(), len(b)),
		yamlDecoder: *yaml.NewDecoder(bytes.NewReader(b)),
	}
}

func (d *Decoder) Decode() (ast.Expr, error) {
	var yn yaml.Node
	if err := d.yamlDecoder.Decode(&yn); err != nil {
		if err == io.EOF {
			// If the input is empty, we produce a single null literal with EOF.
			// Note that when the input contains "---", we get an empty document
			// with a null scalar value inside instead.
			return &ast.BasicLit{
				Kind:  token.NULL,
				Value: "null",
			}, err
		}
		// Unfortunately, yaml.v3's syntax errors are opaque strings,
		// and they only include line numbers in some but not all cases.
		// TODO(mvdan): improve upstream's errors so they are structured
		// and always contain some position information.
		e := err.Error()
		if s, ok := strings.CutPrefix(e, "yaml: line "); ok {
			// From "yaml: line 3: some issue" to "foo.yaml:3: some issue".
			e = d.tokFile.Name() + ":" + s
		} else if s, ok := strings.CutPrefix(e, "yaml:"); ok {
			// From "yaml: some issue" to "foo.yaml: some issue".
			e = d.tokFile.Name() + ":" + s
		} else {
			return nil, err
		}
		return nil, errors.New(e)
	}
	return d.extract(&yn)
}

func Unmarshal(filename string, data []byte) (ast.Expr, error) {
	d := NewDecoder(filename, data)
	x, err := d.Decode()
	if err != nil {
		if err == io.EOF {
			return x, nil
		}
		return x, err
	}
	// TODO(mvdan): fail if there are more documents in the input?
	return x, nil
}

func (d *Decoder) extract(yn *yaml.Node) (ast.Expr, error) {
	d.headComments(yn)
	var expr ast.Expr
	var err error
	switch yn.Kind {
	case yaml.DocumentNode:
		expr, err = d.document(yn)
	case yaml.SequenceNode:
		expr, err = d.sequence(yn)
	case yaml.MappingNode:
		expr, err = d.mapping(yn)
	case yaml.ScalarNode:
		expr, err = d.scalar(yn)
	case yaml.AliasNode:
		expr, err = d.alias(yn)
	default:
		return nil, d.posErrorf(yn, "unknown yaml node kind: %d", yn.Kind)
	}
	if err != nil {
		return nil, err
	}
	d.addComments(expr, yn, 1)
	return expr, nil
}

func (d *Decoder) comments(src string) []*ast.Comment {
	if src == "" {
		return nil
	}
	var comments []*ast.Comment
	for _, line := range strings.Split(src, "\n") {
		if line == "" {
			continue // yaml.v3 comments have a trailing newline at times
		}
		comments = append(comments, &ast.Comment{
			// Note that yaml.v3 does not give us comment positions.
			Text: "//" + line[1:],
		})
	}
	return comments
}

func (d *Decoder) headComments(yn *yaml.Node) {
	comments := d.comments(yn.HeadComment)
	// TODO(mvdan): once yaml.v3 records comment positions,
	// we can better ensure that sections separated by empty lines are kept that way.
	// For now, all we can do is approximate by counting lines,
	// and assuming that head comments are not separated from their node.
	// This will be wrong in some cases, moving empty lines, but is better than nothing.
	if len(d.pendingHeadComments) == 0 && len(comments) > 0 {
		c := comments[0]
		if d.lastPos.IsValid() && (yn.Line-len(comments))-d.lastPos.Line >= 2 {
			c.Slash = c.Slash.WithRel(token.NewSection)
		}
	}
	d.pendingHeadComments = append(d.pendingHeadComments, comments...)
}

func (d *Decoder) addComments(n ast.Node, yn *yaml.Node, linePos int8) {
	// cue/ast and cue/format are not able to attach a comment to a node
	// when the comment immediately follows the node.
	// For some nodes like fields, the best we can do is move the comments up.
	// For the root-level struct, we do want to leave comments
	// at the end of the document to be left at the very end.
	footComments := d.comments(yn.FootComment)
	if _, ok := n.(*ast.StructLit); !ok {
		d.pendingHeadComments = append(d.pendingHeadComments, footComments...)
		footComments = nil
	}
	if comments := d.pendingHeadComments; len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			Doc:      true,
			Position: 0,
			List:     comments,
		})
	}
	if comments := d.comments(yn.LineComment); len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			Line:     true,
			Position: linePos,
			List:     comments,
		})
	}
	if comments := footComments; len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			// After 100 tokens, so that the comment goes after the entire node.
			// TODO(mvdan): this is hacky, can the cue/ast API support trailing comments better?
			Position: 100,
			List:     comments,
		})
	}
	d.pendingHeadComments = nil
}

func (d *Decoder) posErrorf(yn *yaml.Node, format string, args ...any) error {
	// TODO(mvdan): use columns as well; for now they are left out to avoid test churn
	// return fmt.Errorf(d.pos(n).String()+" "+format, args...)
	return fmt.Errorf(d.tokFile.Name()+":"+strconv.Itoa(yn.Line)+": "+format, args...)
}

func (d *Decoder) pos(yn *yaml.Node) token.Pos {
	// Calculate the position's offset via the line and column numbers.
	offset := d.tokLines[yn.Line-1] + (yn.Column - 1)
	pos := d.tokFile.Pos(offset, token.NoRelPos)

	if d.forceNewline {
		d.forceNewline = false
		pos = pos.WithRel(token.Newline)
	} else if d.lastPos.IsValid() {
		switch {
		case yn.Line-d.lastPos.Line >= 2:
			pos = pos.WithRel(token.NewSection)
		case yn.Line-d.lastPos.Line == 1:
			pos = pos.WithRel(token.Newline)
		case yn.Column-d.lastPos.Column > 0:
			pos = pos.WithRel(token.Blank)
		default:
			pos = pos.WithRel(token.NoSpace)
		}
		// If for any reason the node's position is before the last position,
		// give up and return an empty position. Akin to: yn.Pos().Before(d.lastPos)
		//
		// TODO(mvdan): Brought over from the old decoder; when does this happen?
		// Can we get rid of those edge cases and this bit of logic?
		if yn.Line < d.lastPos.Line || (yn.Line == d.lastPos.Line && yn.Column < d.lastPos.Column) {
			return token.NoPos
		}
	}
	d.lastPos = token.Position{Line: yn.Line, Column: yn.Column}
	return pos
}

func (d *Decoder) document(yn *yaml.Node) (ast.Expr, error) {
	if n := len(yn.Content); n != 1 {
		return nil, d.posErrorf(yn, "yaml document nodes are meant to have one content node but have %d", n)
	}
	return d.extract(yn.Content[0])
}

func (d *Decoder) sequence(yn *yaml.Node) (ast.Expr, error) {
	list := &ast.ListLit{
		Lbrack: d.pos(yn).WithRel(token.Blank),
	}
	multiline := false
	if len(yn.Content) > 0 {
		multiline = yn.Line < yn.Content[len(yn.Content)-1].Line
	}

	// If a list is empty, or ends with a struct, the closing `]` is on the same line.
	closeSameLine := true
	for _, c := range yn.Content {
		d.forceNewline = multiline
		elem, err := d.extract(c)
		if err != nil {
			return nil, err
		}
		list.Elts = append(list.Elts, elem)
		// A list of structs begins with `[{`, so let it end with `}]`.
		_, closeSameLine = elem.(*ast.StructLit)
	}
	if multiline && !closeSameLine {
		list.Rbrack = list.Rbrack.WithRel(token.Newline)
	}
	return list, nil
}

func (d *Decoder) mapping(yn *yaml.Node) (ast.Expr, error) {
	strct := &ast.StructLit{}
	multiline := false
	if len(yn.Content) > 0 {
		multiline = yn.Line < yn.Content[len(yn.Content)-1].Line
	}

	if err := d.insertMap(yn, strct, multiline, false); err != nil {
		return nil, err
	}
	// TODO(mvdan): moving these positions above insertMap breaks a few tests, why?
	strct.Lbrace = d.pos(yn).WithRel(token.Blank)
	if multiline {
		strct.Rbrace = strct.Lbrace.WithRel(token.Newline)
	} else {
		strct.Rbrace = strct.Lbrace
	}
	return strct, nil
}

func (d *Decoder) insertMap(yn *yaml.Node, m *ast.StructLit, multiline, mergeValues bool) error {
	l := len(yn.Content)
outer:
	for i := 0; i < l; i += 2 {
		if multiline {
			d.forceNewline = true
		}
		yk, yv := yn.Content[i], yn.Content[i+1]
		d.headComments(yk)
		if isMerge(yk) {
			mergeValues = true
			if err := d.merge(yv, m, multiline); err != nil {
				return err
			}
			continue
		}
		if yk.Kind != yaml.ScalarNode {
			return d.posErrorf(yn, "invalid map key: %v", yk.ShortTag())
		}

		field := &ast.Field{}
		label, err := d.label(yk)
		if err != nil {
			return err
		}
		d.addComments(label, yk, 1)
		field.Label = label

		if mergeValues {
			key := labelStr(label)
			for _, decl := range m.Elts {
				f := decl.(*ast.Field)
				name, _, err := ast.LabelName(f.Label)
				if err == nil && name == key {
					f.Value, err = d.extract(yv)
					if err != nil {
						return err
					}
					continue outer
				}
			}
		}

		value, err := d.extract(yv)
		if err != nil {
			return err
		}
		field.Value = value

		m.Elts = append(m.Elts, field)
	}
	return nil
}

func (d *Decoder) merge(yn *yaml.Node, m *ast.StructLit, multiline bool) error {
	switch yn.Kind {
	case yaml.MappingNode:
		return d.insertMap(yn, m, multiline, true)
	case yaml.AliasNode:
		return d.insertMap(yn.Alias, m, multiline, true)
	case yaml.SequenceNode:
		// Step backwards as earlier nodes take precedence.
		for i := len(yn.Content) - 1; i >= 0; i-- {
			if err := d.merge(yn.Content[i], m, multiline); err != nil {
				return err
			}
		}
		return nil
	default:
		return d.posErrorf(yn, "map merge requires map or sequence of maps as the value")
	}
}

func (d *Decoder) label(yn *yaml.Node) (ast.Label, error) {
	pos := d.pos(yn)

	expr, err := d.scalar(yn)
	if err != nil {
		return nil, err
	}
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			if ast.IsValidIdent(yn.Value) && !internal.IsDefOrHidden(yn.Value) {
				return &ast.Ident{
					NamePos: pos,
					Name:    yn.Value,
				}, nil
			}
			ast.SetPos(expr, pos)
			return expr, nil
		}

		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.Label.Quote(expr.Value),
		}, nil

	default:
		return nil, d.posErrorf(yn, "invalid label "+yn.Value)
	}
}

const (
	// TODO(mvdan): The strings below are from yaml.v3; should we be relying on upstream somehow?
	nullTag      = "!!null"
	boolTag      = "!!bool"
	strTag       = "!!str"
	intTag       = "!!int"
	floatTag     = "!!float"
	timestampTag = "!!timestamp"
	seqTag       = "!!seq"
	mapTag       = "!!map"
	binaryTag    = "!!binary"
	mergeTag     = "!!merge"
)

func (d *Decoder) scalar(yn *yaml.Node) (ast.Expr, error) {
	switch tag := yn.ShortTag(); tag {
	// TODO: use parse literal or parse expression instead.
	case timestampTag:
		return &ast.BasicLit{
			ValuePos: d.pos(yn),
			Kind:     token.STRING,
			Value:    literal.String.Quote(yn.Value),
		}, nil
	case strTag:
		return &ast.BasicLit{
			ValuePos: d.pos(yn),
			Kind:     token.STRING,
			Value:    quoteString(yn.Value),
		}, nil

	case binaryTag:
		data, err := base64.StdEncoding.DecodeString(yn.Value)
		if err != nil {
			return nil, d.posErrorf(yn, "!!binary value contains invalid base64 data")
		}
		return &ast.BasicLit{
			ValuePos: d.pos(yn),
			Kind:     token.STRING,
			Value:    literal.Bytes.Quote(string(data)),
		}, nil

	case boolTag:
		t := false
		switch yn.Value {
		// TODO(mvdan): The strings below are from yaml.v3; should we be relying on upstream somehow?
		case "true", "True", "TRUE":
			t = true
		}
		lit := ast.NewBool(t)
		lit.ValuePos = d.pos(yn)
		return lit, nil

	case intTag:
		// Convert YAML octal to CUE octal. If YAML accepted an invalid
		// integer, just convert it as well to ensure CUE will fail.
		value := yn.Value
		if len(value) > 1 && value[0] == '0' && value[1] <= '9' {
			value = "0o" + value[1:]
		}
		var info literal.NumInfo
		// We make the assumption that any valid YAML integer literal will be a valid
		// CUE integer literal as well, with the only exception of octal numbers above.
		// Note that `!!int 123.456` is not allowed.
		if err := literal.ParseNum(value, &info); err != nil || !info.IsInt() {
			return nil, d.posErrorf(yn, "cannot decode %q as %s", value, yn.ShortTag())
		}
		return d.makeNum(yn, value, token.INT), nil

	case floatTag:
		value := yn.Value
		// TODO(mvdan): The strings below are from yaml.v3; should we be relying on upstream somehow?
		switch value {
		case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
			value = "+Inf"
		case "-.inf", "-.Inf", "-.INF":
			value = "-Inf"
		case ".nan", ".NaN", ".NAN":
			value = "NaN"
		default:
			var info literal.NumInfo
			// We make the assumption that any valid YAML float literal will be a valid
			// CUE float literal as well, with the only exception of Inf/NaN above.
			// Note that `!!float 123` is allowed.
			if err := literal.ParseNum(value, &info); err != nil {
				return nil, d.posErrorf(yn, "cannot decode %q as %s", value, yn.ShortTag())
			}
			// If the decoded YAML scalar was explicitly or implicitly a float,
			// and the scalar literal looks like an integer,
			// unify it with "number" to record the fact that it was represented as a float.
			// Don't unify with float, as `float & 123` is invalid, and there's no need
			// to forbid representing the number as an integer either.
			if yn.Tag != "" {
				if p := strings.IndexAny(value, ".eEiInN"); p == -1 {
					// TODO: number(v) when we have conversions
					// TODO(mvdan): don't shove the unification inside a BasicLit.Value string
					value = fmt.Sprintf("number & %s", value)
				}
			}
		}
		return d.makeNum(yn, value, token.FLOAT), nil

	case nullTag:
		return &ast.BasicLit{
			ValuePos: d.pos(yn).WithRel(token.Blank),
			Kind:     token.NULL,
			Value:    "null",
		}, nil
	default:
		return nil, d.posErrorf(yn, "cannot unmarshal tag %q", tag)
	}
}

func (d *Decoder) makeNum(yn *yaml.Node, val string, kind token.Token) (expr ast.Expr) {
	val, negative := strings.CutPrefix(val, "-")
	expr = &ast.BasicLit{
		ValuePos: d.pos(yn),
		Kind:     kind,
		Value:    val,
	}
	if negative {
		expr = &ast.UnaryExpr{
			OpPos: d.pos(yn),
			Op:    token.SUB,
			X:     expr,
		}
	}
	return expr
}

func (d *Decoder) alias(yn *yaml.Node) (ast.Expr, error) {
	if d.extractingAliases[yn] {
		// TODO this could actually be allowed in some circumstances.
		return nil, d.posErrorf(yn, "anchor %q value contains itself", yn.Value)
	}
	if d.extractingAliases == nil {
		d.extractingAliases = make(map[*yaml.Node]bool)
	}
	d.extractingAliases[yn] = true
	var node ast.Expr
	node, err := d.extract(yn.Alias)
	delete(d.extractingAliases, yn)
	return node, err
}

// quoteString converts a string to a CUE multiline string if needed.
// TODO(mvdan): this is brought over from the old decoder; we should consider
// polishing this API and moving it someplace better like cue/literal.
func quoteString(s string) string {
	lines := []string{}
	last := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[last:i])
			last = i + 1
		}
		if c == '\r' {
			goto quoted
		}
	}
	lines = append(lines, s[last:])
	if len(lines) >= 2 {
		buf := []byte{}
		buf = append(buf, `"""`+"\n"...)
		for _, l := range lines {
			if l == "" {
				// no indentation for empty lines
				buf = append(buf, '\n')
				continue
			}
			buf = append(buf, '\t')
			p := len(buf)
			// TODO(mvdan): do not use Go's strconv for CUE syntax.
			buf = strconv.AppendQuote(buf, l)
			// remove quotes
			buf[p] = '\t'
			buf[len(buf)-1] = '\n'
		}
		buf = append(buf, "\t\t"+`"""`...)
		return string(buf)
	}
quoted:
	return literal.String.Quote(s)
}

func labelStr(l ast.Label) string {
	switch l := l.(type) {
	case *ast.Ident:
		return l.Name
	case *ast.BasicLit:
		s, _ := literal.Unquote(l.Value)
		return s
	}
	return ""
}

func isMerge(yn *yaml.Node) bool {
	// TODO(mvdan): The boolean logic below is from yaml.v3; should we be relying on upstream somehow?
	return yn.Kind == yaml.ScalarNode && yn.Value == "<<" && (yn.Tag == "" || yn.Tag == "!" || yn.ShortTag() == mergeTag)
}
