package telemetry

import "fmt"

// exprNode 为编译后的条件表达式语法树节点。
// values 按比较项在规则中的下标给出当前真值。
type exprNode interface {
	eval(values []bool) bool
}

type orNode struct {
	left, right exprNode
}

type andNode struct {
	left, right exprNode
}

type leafNode struct {
	idx int
}

func (n orNode) eval(values []bool) bool   { return n.left.eval(values) || n.right.eval(values) }
func (n andNode) eval(values []bool) bool  { return n.left.eval(values) && n.right.eval(values) }
func (n leafNode) eval(values []bool) bool { return values[n.idx] }

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

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '.' || c == '-'
}

func lexExpr(s string) ([]token, error) {
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
			i++
			if i < len(s) && s[i] == '&' {
				i++
			}
			toks = append(toks, token{kind: tokAnd})
		case c == '|':
			i++
			if i < len(s) && s[i] == '|' {
				i++
			}
			toks = append(toks, token{kind: tokOr})
		case isIdentStart(c):
			j := i + 1
			for j < len(s) && isIdentChar(s[j]) {
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
			return nil, fmt.Errorf("telemetry: 表达式含非法字符 %q", string(c))
		}
	}
	return toks, nil
}

type exprParser struct {
	toks []token
	pos  int
	refs map[string]int
}

// parseExpr 解析条件表达式，refs 将比较项名映射到其下标。
// 文法：expr := orExpr；orExpr := andExpr (OR andExpr)*；andExpr := primary (AND primary)*；
// primary := '(' expr ')' | IDENT。AND 优先级高于 OR。
func parseExpr(expr string, refs map[string]int) (exprNode, error) {
	toks, err := lexExpr(expr)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("telemetry: 表达式为空")
	}
	p := &exprParser{toks: toks, refs: refs}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("telemetry: 表达式在 %q 后存在多余内容", p.toks[p.pos].text)
	}
	return node, nil
}

func (p *exprParser) parseOr() (exprNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.pos < len(p.toks) && p.toks[p.pos].kind == tokOr {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orNode{left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseAnd() (exprNode, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.pos < len(p.toks) && p.toks[p.pos].kind == tokAnd {
		p.pos++
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = andNode{left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parsePrimary() (exprNode, error) {
	if p.pos >= len(p.toks) {
		return nil, fmt.Errorf("telemetry: 表达式意外结束")
	}
	tok := p.toks[p.pos]
	switch tok.kind {
	case tokIdent:
		idx, ok := p.refs[tok.text]
		if !ok {
			return nil, fmt.Errorf("telemetry: 表达式引用了未定义的比较项 %q", tok.text)
		}
		p.pos++
		return leafNode{idx: idx}, nil
	case tokLParen:
		p.pos++
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.pos >= len(p.toks) || p.toks[p.pos].kind != tokRParen {
			return nil, fmt.Errorf("telemetry: 表达式缺少右括号")
		}
		p.pos++
		return node, nil
	default:
		return nil, fmt.Errorf("telemetry: 表达式在 %q 处语法错误", tok.text)
	}
}
