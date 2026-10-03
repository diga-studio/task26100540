package telemetry

import (
	"reflect"
	"testing"
)

func cmpX(name, metric string, upper, lower, validityMs float64) Comparison {
	return Comparison{Name: name, Metric: metric, Upper: upper, Lower: lower, ValidityMs: int64(validityMs)}
}

func TestOrBranchHandoffSameTimestamp(t *testing.T) {
	// 与、或组合中分支同刻交接：A 退出、B 接管，表达式最终仍为真，累计不能重置。
	comps := []Comparison{cmpX("A", "a", 10, 5, 100), cmpX("B", "b", 20, 10, 100)}
	e := mustEngine(t, testRule("A | B", 50, comps, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))                    // A 真，条件为真，自 t=0 累计
	mustAdvance(t, e, 100, rd("a", 30, 0), rd("b", 30, 20)) // t=30 同刻交接，条件保持真
	wantTriggers(t, e, 50)
}

func TestNestedBranchHandoffSameTimestamp(t *testing.T) {
	// 嵌套表达式 A & (B | C)：B 退出、C 接管，整体保持真。
	comps := []Comparison{cmpX("A", "a", 10, 5, 100), cmpX("B", "b", 20, 10, 100), cmpX("C", "c", 15, 8, 100)}
	e := mustEngine(t, testRule("A & (B | C)", 50, comps, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10), rd("b", 0, 20))
	mustAdvance(t, e, 100, rd("b", 30, 0), rd("c", 30, 15))
	wantTriggers(t, e, 50)
}

func TestReTriggerAfterConditionFalse(t *testing.T) {
	// 变假后取消累计并重新允许触发；连续为真期间只触发一次。
	comps := []Comparison{cmpX("A", "a", 10, 5, 100), cmpX("B", "b", 20, 10, 100)}
	e := mustEngine(t, testRule("A | B", 50, comps, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))
	mustAdvance(t, e, 30, rd("a", 30, 0))   // 条件变假，取消累计
	mustAdvance(t, e, 100, rd("b", 40, 20)) // t=40 重新为真，t=90 触发
	wantTriggers(t, e, 90)
	mustAdvance(t, e, 300, rd("b", 200, 20)) // t=140 B 失效变假；t=200 再次为真，t=250 再次触发
	wantTriggers(t, e, 90, 250)
}

func TestAndRequiresBothBranches(t *testing.T) {
	comps := []Comparison{cmpX("A", "a", 10, 5, 100), cmpX("B", "b", 20, 10, 100)}
	e := mustEngine(t, testRule("A & B", 50, comps, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))    // 仅 A 真，条件为假
	mustAdvance(t, e, 100, rd("b", 30, 20)) // t=30 条件才为真，t=80 触发
	wantTriggers(t, e, 80)
}

func TestBatchingDeterminism(t *testing.T) {
	comps := []Comparison{cmpX("A", "a", 10, 5, 60), cmpX("B", "b", 20, 10, 80)}
	newRule := func() Rule {
		return Rule{
			ID:          "rule1",
			DeviceIDs:   []string{"dev1", "dev2"},
			Comparisons: comps,
			Expression:  "A | B",
			DurationMs:  40,
			Intervals:   []Interval{{0, 150}, {150, 400}},
		}
	}
	rd2 := func(dev, metric string, timeMs int64, value float64) Reading {
		return Reading{DeviceID: dev, Metric: metric, TimeMs: timeMs, Value: value}
	}
	all := []Reading{
		rd2("dev1", "a", 0, 10), rd2("dev1", "b", 10, 20), rd2("dev1", "a", 30, 0),
		rd2("dev1", "b", 90, 5), rd2("dev1", "a", 100, 12), rd2("dev1", "a", 160, 10),
		rd2("dev1", "b", 170, 25), rd2("dev1", "a", 220, 0),
		rd2("dev2", "a", 5, 10), rd2("dev2", "a", 50, 0), rd2("dev2", "a", 200, 10),
	}
	want := []Trigger{
		{RuleID: "rule1", DeviceID: "dev1", TimeMs: 40},
		{RuleID: "rule1", DeviceID: "dev2", TimeMs: 45},
		{RuleID: "rule1", DeviceID: "dev1", TimeMs: 140},
		{RuleID: "rule1", DeviceID: "dev1", TimeMs: 200},
		{RuleID: "rule1", DeviceID: "dev2", TimeMs: 240},
	}

	// 一次性历史回放
	e1 := mustEngine(t, newRule())
	mustAdvance(t, e1, 400, all...)

	// 多批推进，插入空水位
	e2 := mustEngine(t, newRule())
	mustAdvance(t, e2, 10, all[0], all[1], all[8])
	mustAdvance(t, e2, 25)
	mustAdvance(t, e2, 100, all[2], all[3], all[4], all[9])
	mustAdvance(t, e2, 155)
	mustAdvance(t, e2, 400, all[5], all[6], all[7], all[10])

	// 同批乱序
	shuffled := []Reading{all[7], all[3], all[10], all[0], all[5], all[9], all[1], all[8], all[2], all[6], all[4]}
	e3 := mustEngine(t, newRule())
	mustAdvance(t, e3, 400, shuffled...)

	for i, e := range []*Engine{e1, e2, e3} {
		if got := e.Triggers(); !reflect.DeepEqual(got, want) {
			t.Fatalf("引擎 %d 触发记录 = %v, 期望 %v", i+1, got, want)
		}
	}
}
