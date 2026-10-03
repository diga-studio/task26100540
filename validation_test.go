package telemetry

import "testing"

func validRule() Rule {
	return Rule{
		ID:          "rule1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmpX("A", "a", 10, 5, 100)},
		Expression:  "A",
		DurationMs:  50,
		Intervals:   []Interval{{0, 1000}},
	}
}

func TestInvalidRules(t *testing.T) {
	cases := map[string]func(r *Rule){
		"空规则ID":     func(r *Rule) { r.ID = "" },
		"空设备ID":     func(r *Rule) { r.DeviceIDs = []string{""} },
		"重复设备":      func(r *Rule) { r.DeviceIDs = []string{"dev1", "dev1"} },
		"无比较项":      func(r *Rule) { r.Comparisons = nil },
		"比较项无名":     func(r *Rule) { r.Comparisons[0].Name = "" },
		"比较项重名":     func(r *Rule) { r.Comparisons = append(r.Comparisons, r.Comparisons[0]) },
		"上阈值不大于下阈值": func(r *Rule) { r.Comparisons[0].Upper = 5 },
		"有效期非正":     func(r *Rule) { r.Comparisons[0].ValidityMs = 0 },
		"持续时长非正":    func(r *Rule) { r.DurationMs = 0 },
		"表达式为空":     func(r *Rule) { r.Expression = "" },
		"表达式引用未知项":  func(r *Rule) { r.Expression = "A & B" },
		"区间起止无效":    func(r *Rule) { r.Intervals = []Interval{{100, 50}} },
		"区间重叠":      func(r *Rule) { r.Intervals = []Interval{{0, 100}, {50, 200}} },
	}
	for name, mutate := range cases {
		rule := validRule()
		mutate(&rule)
		if _, err := NewEngine([]Rule{rule}); err == nil {
			t.Fatalf("用例 %q 应校验失败", name)
		}
	}
	if _, err := NewEngine([]Rule{validRule(), validRule()}); err == nil {
		t.Fatal("重复规则 ID 应校验失败")
	}
	if _, err := NewEngine([]Rule{validRule()}); err != nil {
		t.Fatalf("合法规则不应报错: %v", err)
	}
	// 首尾相接的区间合法
	rule := validRule()
	rule.Intervals = []Interval{{0, 100}, {100, 200}}
	if _, err := NewEngine([]Rule{rule}); err != nil {
		t.Fatalf("相邻区间不应报错: %v", err)
	}
}

func TestAdvanceErrors(t *testing.T) {
	e := mustEngine(t, validRule())
	mustAdvance(t, e, 100, rd("a", 50, 10))
	if err := e.Advance(nil, 99); err == nil {
		t.Fatal("水位回退应报错")
	}
	if err := e.Advance([]Reading{rd("a", 100, 10)}, 200); err == nil {
		t.Fatal("不晚于上一水位的读数应报错")
	}
	if err := e.Advance([]Reading{rd("a", 300, 10)}, 200); err == nil {
		t.Fatal("超过水位的读数应报错")
	}
	// 报错后状态不被污染，仍可正常推进
	mustAdvance(t, e, 200, rd("a", 150, 10))
}
