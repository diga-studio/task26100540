package telemetry

import (
	"reflect"
	"testing"
)

func cmp(name, metric string, upper, lower float64, validity int64) Comparison {
	return Comparison{Name: name, Metric: metric, Upper: upper, Lower: lower, Validity: validity}
}

func rd(dev, metric string, time int64, value float64) Reading {
	return Reading{DeviceID: dev, Metric: metric, Time: time, Value: value}
}

func trig(ruleID, dev string, time int64) Trigger {
	return Trigger{RuleID: ruleID, DeviceID: dev, Time: time}
}

func mustEngine(t *testing.T, rules ...Rule) *Engine {
	t.Helper()
	e, err := NewEngine(rules)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func checkTriggers(t *testing.T, e *Engine, want []Trigger) {
	t.Helper()
	got := e.Triggers()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("triggers = %v, want %v", got, want)
	}
}

// 与、或组合中分支同刻交接，表达式最终仍为真时累计不能重置。
func TestBranchHandoffSameTimeKeepsAccumulation(t *testing.T) {
	rule := Rule{
		ID:        "r1",
		DeviceIDs: []string{"dev1"},
		Comparisons: []Comparison{
			cmp("a", "m1", 10, 5, 10),
			cmp("b", "m2", 10, 5, 100),
			cmp("c", "m3", 10, 5, 100),
		},
		Expression: "(a OR b) AND c",
		Duration:   20,
		Intervals:  []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 0, 15),  // a 置真，t=10 失效
		rd("dev1", "m3", 0, 15),  // c 置真
		rd("dev1", "m2", 10, 15), // t=10：a 失效与 b 置真同刻交接，表达式仍为真
	}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 20)})
}

// 对照：交接存在间隙时累计重新开始。
func TestBranchHandoffWithGapResetsAccumulation(t *testing.T) {
	rule := Rule{
		ID:        "r1",
		DeviceIDs: []string{"dev1"},
		Comparisons: []Comparison{
			cmp("a", "m1", 10, 5, 10),
			cmp("b", "m2", 10, 5, 100),
			cmp("c", "m3", 10, 5, 100),
		},
		Expression: "(a OR b) AND c",
		Duration:   20,
		Intervals:  []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 0, 15),
		rd("dev1", "m3", 0, 15),
		rd("dev1", "m2", 11, 15), // t=10 条件变假，t=11 重新为真
	}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 31)})
}

// 持续期限与读数刷新重合：同刻刷新不造成瞬时失效，正常触发。
func TestDurationCoincidesWithRefresh(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 10)},
		Expression:  "a",
		Duration:    10,
		Intervals:   []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 0, 15),
		rd("dev1", "m1", 10, 15), // 与失效、持续期限同刻的刷新
	}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 10)})
}

// 持续期限与读数失效重合：失效先处理，条件变假，不触发。
func TestDurationCoincidesWithExpiry(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 10)},
		Expression:  "a",
		Duration:    10,
		Intervals:   []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{rd("dev1", "m1", 0, 15)}, 100)
	checkTriggers(t, e, nil)
}

// 持续期限与生效区间结束重合：区间先切换，累计取消，不触发。
func TestDurationCoincidesWithIntervalEnd(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 100)},
		Expression:  "a",
		Duration:    10,
		Intervals:   []Interval{{Start: 0, End: 10}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{rd("dev1", "m1", 0, 15)}, 100)
	checkTriggers(t, e, nil)
}

// 对照：区间多留 1ms，持续期限在区间内到达，触发。
func TestDurationJustBeforeIntervalEnd(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 100)},
		Expression:  "a",
		Duration:    10,
		Intervals:   []Interval{{Start: 0, End: 11}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{rd("dev1", "m1", 0, 15)}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 10)})
}

// 持续期限到达的同一时刻条件变假，取消触发。
func TestConditionFalseAtTriggerTime(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 100)},
		Expression:  "a",
		Duration:    10,
		Intervals:   []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 0, 15),
		rd("dev1", "m1", 10, 1), // t=10 读数使条件变假，与持续期限同刻
	}, 100)
	checkTriggers(t, e, nil)
}
