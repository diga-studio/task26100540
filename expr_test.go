package telemetry

import "testing"

func evalExpr(t *testing.T, expr string, refs map[string]int, values []bool) bool {
	t.Helper()
	node, err := parseExpr(expr, refs)
	if err != nil {
		t.Fatalf("parseExpr(%q) 出错: %v", expr, err)
	}
	return node.eval(values)
}

func TestExprPrecedenceAndParens(t *testing.T) {
	refs := map[string]int{"A": 0, "B": 1, "C": 2}
	// & 优先级高于 |：A | B & C 等价于 A | (B & C)
	if !evalExpr(t, "A | B & C", refs, []bool{true, false, false}) {
		t.Fatal("A | B & C 在 A=true 时应为真")
	}
	if evalExpr(t, "A | B & C", refs, []bool{false, true, false}) {
		t.Fatal("A | B & C 在仅 B=true 时应为假")
	}
	// 括号改变优先级：(A | B) & C
	if evalExpr(t, "(A | B) & C", refs, []bool{true, false, false}) {
		t.Fatal("(A | B) & C 在 C=false 时应为假")
	}
	if !evalExpr(t, "(A | B) & C", refs, []bool{false, true, true}) {
		t.Fatal("(A | B) & C 在 B=true,C=true 时应为真")
	}
}

func TestExprOperatorForms(t *testing.T) {
	refs := map[string]int{"A": 0, "B": 1}
	for _, expr := range []string{"A & B", "A && B", "A AND B", "A and B"} {
		if !evalExpr(t, expr, refs, []bool{true, true}) {
			t.Fatalf("%s 应为真", expr)
		}
		if evalExpr(t, expr, refs, []bool{true, false}) {
			t.Fatalf("%s 应为假", expr)
		}
	}
	for _, expr := range []string{"A | B", "A || B", "A OR B", "A or B"} {
		if !evalExpr(t, expr, refs, []bool{false, true}) {
			t.Fatalf("%s 应为真", expr)
		}
		if evalExpr(t, expr, refs, []bool{false, false}) {
			t.Fatalf("%s 应为假", expr)
		}
	}
}

func TestExprErrors(t *testing.T) {
	refs := map[string]int{"A": 0}
	for _, expr := range []string{"", "A &", "& A", "A B", "(A", "A)", "B", "A & B"} {
		if _, err := parseExpr(expr, refs); err == nil {
			t.Fatalf("parseExpr(%q) 应返回错误", expr)
		}
	}
}
