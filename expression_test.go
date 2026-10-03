package telemetry

import "testing"

func evalExpr(t *testing.T, src string, items map[string]bool) bool {
	t.Helper()
	root, err := parseExpression(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return root.eval(items)
}

func TestExpressionPrecedence(t *testing.T) {
	// AND 优先于 OR：a OR b AND c 等价于 a OR (b AND c)
	if !evalExpr(t, "a OR b AND c", map[string]bool{"a": true}) {
		t.Fatal("a=true should satisfy")
	}
	if evalExpr(t, "a OR b AND c", map[string]bool{"b": true}) {
		t.Fatal("b alone should not satisfy")
	}
	if !evalExpr(t, "a OR b AND c", map[string]bool{"b": true, "c": true}) {
		t.Fatal("b and c should satisfy")
	}
}

func TestExpressionParensAndSymbols(t *testing.T) {
	if !evalExpr(t, "(a || b) && c", map[string]bool{"b": true, "c": true}) {
		t.Fatal("(a||b)&&c should hold")
	}
	if evalExpr(t, "(a || b) && c", map[string]bool{"a": true, "b": true}) {
		t.Fatal("missing c should not hold")
	}
	if !evalExpr(t, "((a))", map[string]bool{"a": true}) {
		t.Fatal("nested parens")
	}
}

func TestExpressionErrors(t *testing.T) {
	for _, src := range []string{"", "a AND", "(a", "a b", "a OR OR b", ")", "a & b"} {
		if _, err := parseExpression(src); err == nil {
			t.Fatalf("expected error for %q", src)
		}
	}
}
