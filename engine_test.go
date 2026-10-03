package telemetry

import "testing"

func mustEngine(t *testing.T, rules ...Rule) *Engine {
	t.Helper()
	e, err := NewEngine(rules)
	if err != nil {
		t.Fatalf("NewEngine 出错: %v", err)
	}
	return e
}

func mustAdvance(t *testing.T, e *Engine, watermarkMs int64, readings ...Reading) {
	t.Helper()
	if err := e.Advance(readings, watermarkMs); err != nil {
		t.Fatalf("Advance(watermark=%d) 出错: %v", watermarkMs, err)
	}
}

func wantTriggers(t *testing.T, e *Engine, want ...int64) {
	t.Helper()
	got := e.Triggers()
	if len(got) != len(want) {
		t.Fatalf("触发记录数 = %d (%v), 期望 %d (%v)", len(got), got, len(want), want)
	}
	for i, tr := range got {
		if tr.TimeMs != want[i] {
			t.Fatalf("第 %d 条触发时刻 = %d, 期望 %d (全部: %v)", i, tr.TimeMs, want[i], got)
		}
	}
}

func testRule(expr string, durationMs int64, comps []Comparison, intervals []Interval) Rule {
	return Rule{
		ID:          "rule1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: comps,
		Expression:  expr,
		DurationMs:  durationMs,
		Intervals:   intervals,
	}
}

func cmpA(validityMs int64) Comparison {
	return Comparison{Name: "A", Metric: "a", Upper: 10, Lower: 5, ValidityMs: validityMs}
}

func rd(metric string, timeMs int64, value float64) Reading {
	return Reading{DeviceID: "dev1", Metric: metric, TimeMs: timeMs, Value: value}
}

func TestHysteresis(t *testing.T) {
	e := mustEngine(t, testRule("A", 10, []Comparison{cmpA(1000)}, []Interval{{0, 10000}}))
	mustAdvance(t, e, 5, rd("a", 0, 7))    // 阈值之间，保持假
	mustAdvance(t, e, 10, rd("a", 10, 10)) // 达到上阈值，置真
	mustAdvance(t, e, 30, rd("a", 15, 7))  // 阈值之间，保持真；t=20 触发
	wantTriggers(t, e, 20)
	mustAdvance(t, e, 40, rd("a", 40, 5))  // 达到下阈值，置假
	mustAdvance(t, e, 100, rd("a", 45, 6)) // 阈值之间，保持假
	wantTriggers(t, e, 20)
}

func TestTriggerWithoutSampleAtTriggerTime(t *testing.T) {
	e := mustEngine(t, testRule("A", 50, []Comparison{cmpA(1000)}, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))
	mustAdvance(t, e, 100) // 只推进水位，触发时刻没有采样
	wantTriggers(t, e, 50)
}

func TestRefreshAtDurationBoundary(t *testing.T) {
	// 持续期限与读数刷新同刻：旧读数到期但被同刻新读数刷新，条件保持真，应触发。
	e := mustEngine(t, testRule("A", 50, []Comparison{cmpA(50)}, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))
	mustAdvance(t, e, 100, rd("a", 50, 10))
	wantTriggers(t, e, 50)
}

func TestRefreshBetweenThresholdsKeepsState(t *testing.T) {
	// 刷新值位于阈值之间：保持原状态并延长有效期。
	e := mustEngine(t, testRule("A", 60, []Comparison{cmpA(50)}, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))
	mustAdvance(t, e, 100, rd("a", 40, 7)) // t=40 刷新，有效期延至 90，t=60 触发
	wantTriggers(t, e, 60)
}

func TestExpiryAtDurationBoundary(t *testing.T) {
	// 持续期限与读数失效同刻：失效先处理，条件变假，不触发。
	e := mustEngine(t, testRule("A", 50, []Comparison{cmpA(50)}, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))
	mustAdvance(t, e, 100)
	wantTriggers(t, e)
}

func TestIntervalEndAtDurationBoundary(t *testing.T) {
	// 持续期限与生效区间结束同刻：区间结束先处理，取消累计，不触发。
	e := mustEngine(t, testRule("A", 50, []Comparison{cmpA(1000)}, []Interval{{0, 50}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))
	mustAdvance(t, e, 100)
	wantTriggers(t, e)
}

func TestConditionFalseAtDurationBoundary(t *testing.T) {
	// 持续期限与条件变假同刻：不触发。
	e := mustEngine(t, testRule("A", 50, []Comparison{cmpA(1000)}, []Interval{{0, 1000}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))
	mustAdvance(t, e, 100, rd("a", 50, 0))
	wantTriggers(t, e)
}

func TestAdjacentIntervalsRestart(t *testing.T) {
	// 首尾相接的区间也要重新开始：读数有效期覆盖两个区间，但状态不得继承。
	e := mustEngine(t, testRule("A", 30, []Comparison{cmpA(500)}, []Interval{{0, 100}, {100, 200}}))
	mustAdvance(t, e, 0, rd("a", 0, 10))     // 区间一内 t=30 触发
	mustAdvance(t, e, 300, rd("a", 150, 10)) // 区间二从空白状态开始，t=180 再次触发
	wantTriggers(t, e, 30, 180)
}

func TestReadingsAtIntervalBounds(t *testing.T) {
	// 区间左闭右开：起点读数生效，终点读数忽略，区间外读数忽略。
	e := mustEngine(t, testRule("A", 30, []Comparison{cmpA(1000)}, []Interval{{100, 200}}))
	mustAdvance(t, e, 300, rd("a", 50, 10), rd("a", 100, 10), rd("a", 200, 10))
	wantTriggers(t, e, 130)
}
