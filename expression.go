package telemetry

import "fmt"

// expr 是条件表达式的语法树节点。
type expr interface {
	eval(items map[string]bool) bool
}

type orExpr struct{ left, right expr }
type andExpr struct{ left, right expr }
type refExpr struct{ name string }

func (e orExpr) eval(items map[string]bool) bool  { return e.left.eval(items) || e.right.eval(items) }
func (e andExpr) eval(items map[string]bool) bool { return e.left.eval(items) && e.right.eval(items) }
func (e refExpr) eval(items map[string]bool) bool { return items[e.name] }

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokAnd
	tokOr
	tokLParen
	tokRParen
)

type token struct {
	kind tokenKind
	text string
}

func tokenize(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{kind: tokLParen})
			i++
		case c == ')':
			toks = append(toks, token{kind: tokRParen})
			i++
		case c == '&':
			if i+1 >= len(s) || s[i+1] != '&' {
				return nil, fmt.Errorf("unexpected '&' at offset %d", i)
			}
			toks = append(toks, token{kind: tokAnd})
			i += 2
		case c == '|':
			if i+1 >= len(s) || s[i+1] != '|' {
				return nil, fmt.Errorf("unexpected '|' at offset %d", i)
			}
			toks = append(toks, token{kind: tokOr})
			i += 2
		case isIdentStart(c):
			j := i + 1
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			word := s[i:j]
			switch word {
			case "AND", "and", "And":
				toks = append(toks, token{kind: tokAnd})
			case "OR", "or", "Or":
				toks = append(toks, token{kind: tokOr})
			default:
				toks = append(toks, token{kind: tokIdent, text: word})
			}
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q at offset %d", c, i)
		}
	}
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '/'
}

// parser 是递归下降解析器，AND 优先级高于 OR。
type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() (token, bool) {
	if p.pos < len(p.toks) {
		return p.toks[p.pos], true
	}
	return token{}, false
}

func (p *parser) next() (token, bool) {
	t, ok := p.peek()
	if ok {
		p.pos++
	}
	return t, ok
}

// parseExpression 解析由比较项名字、AND/OR（或 &&/||）及括号组成的表达式。
func parseExpression(s string) (expr, error) {
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	p := &parser{toks: toks}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("unexpected trailing token in expression")
	}
	return root, nil
}

func (p *parser) parseOr() (expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		if t, ok := p.peek(); ok && t.kind == tokOr {
			p.next()
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			left = orExpr{left: left, right: right}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseAnd() (expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		if t, ok := p.peek(); ok && t.kind == tokAnd {
			p.next()
			right, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			left = andExpr{left: left, right: right}
			continue
		}
		return left, nil
	}
}

func (p *parser) parsePrimary() (expr, error) {
	t, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	switch t.kind {
	case tokIdent:
		return refExpr{name: t.text}, nil
	case tokLParen:
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		closing, ok := p.next()
		if !ok || closing.kind != tokRParen {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		return inner, nil
	default:
		return nil, fmt.Errorf("unexpected token in expression")
	}
}
