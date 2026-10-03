package telemetry

import (
	"reflect"
	"testing"
)

// 滞回：>=上阈值置真，<=下阈值置假，两者之间保持原状态；初始为假。
func TestHysteresis(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 1000)},
		Expression:  "a",
		Duration:    10,
		Intervals:   []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 0, 7),  // 两者之间，保持初始假
		rd("dev1", "m1", 5, 12), // >= 上阈值，置真，since=5
		rd("dev1", "m1", 8, 7),  // 两者之间，保持真
	}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 15)})

	e.Advance([]Reading{
		rd("dev1", "m1", 120, 6), // 两者之间，保持真
		rd("dev1", "m1", 125, 5), // <= 下阈值，置假
		rd("dev1", "m1", 130, 9), // 两者之间，保持假
	}, 200)
	// 已触发过的连续为真期间不重复触发；变假后无恢复记录
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 15)})

	e.Advance([]Reading{rd("dev1", "m1", 240, 10)}, 300) // 重新置真，since=240
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 15), trig("r1", "dev1", 250)})
}

// 读数有效期：失效后立即置假；同刻刷新不造成瞬时失效。
func TestValidityExpiry(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 10)},
		Expression:  "a",
		Duration:    25,
		Intervals:   []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 0, 15),  // 有效至 t=10
		rd("dev1", "m1", 10, 15), // 同刻刷新，有效至 t=20
		rd("dev1", "m1", 21, 15), // t=20 已失效，条件在 [20,21) 为假，since=21
		rd("dev1", "m1", 30, 15), // 失效前刷新
		rd("dev1", "m1", 40, 15), // 与失效同刻刷新
	}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 46)})
}

// 生效区间：区间外读数被忽略；首尾相接的区间也要清空状态重新开始。
func TestAdjacentIntervalsRestartState(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 1000)},
		Expression:  "a",
		Duration:    5,
		Intervals:   []Interval{{Start: 0, End: 10}, {Start: 10, End: 20}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 5, 15), // 第一个区间内置真，t=10 区间结束取消累计
	}, 9)
	checkTriggers(t, e, nil)

	// 第二个区间内的读数重新开始累计；若状态被继承，t=15 会误触发
	e.Advance([]Reading{rd("dev1", "m1", 12, 15)}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 17)})
}

// 区间外的读数被忽略；区间左闭右开。
func TestReadingsOutsideIntervalIgnored(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 1000)},
		Expression:  "a",
		Duration:    5,
		Intervals:   []Interval{{Start: 100, End: 200}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 50, 15),  // 区间开始前，忽略
		rd("dev1", "m1", 100, 15), // 左闭，生效，since=100
		rd("dev1", "m1", 200, 15), // 右开，忽略
	}, 300)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 105)})
}

// 触发时刻没有采样也要产生记录；连续为真只触发一次。
func TestTriggerWithoutSampleAtTriggerTime(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 1000)},
		Expression:  "a",
		Duration:    7,
		Intervals:   []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{rd("dev1", "m1", 0, 15)}, 50)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 7)})
	// 推进空水位不重复触发
	e.Advance(nil, 500)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 7)})
}

// 多设备独立求值。
func TestDevicesEvaluatedIndependently(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1", "dev2"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 1000)},
		Expression:  "a",
		Duration:    5,
		Intervals:   []Interval{{Start: 0, End: 1000}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{
		rd("dev1", "m1", 0, 15),
		rd("dev2", "m1", 3, 15),
		rd("dev3", "m1", 0, 15), // 未绑定设备，忽略
	}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 5), trig("r1", "dev2", 8)})
}

// 分批、空水位推进与一次性回放产生完全一致的触发记录。
func TestBatchingInvariance(t *testing.T) {
	rules := []Rule{
		{
			ID:        "r1",
			DeviceIDs: []string{"dev1", "dev2"},
			Comparisons: []Comparison{
				cmp("a", "m1", 10, 5, 15),
				cmp("b", "m2", 20, 8, 25),
				cmp("c", "m3", 3, 1, 10),
			},
			Expression: "(a AND b) OR c",
			Duration:   12,
			Intervals:  []Interval{{Start: 0, End: 60}, {Start: 60, End: 200}},
		},
	}

	readings := []Reading{
		rd("dev1", "m1", 1, 15),   // 0: a 置真
		rd("dev1", "m2", 2, 25),   // 1: b 置真，a AND b 成立，since=2
		rd("dev1", "m1", 16, 15),  // 2: 与上一读数失效同刻刷新
		rd("dev1", "m2", 30, 1),   // 3: b 变假
		rd("dev1", "m3", 35, 5),   // 4: c 置真，分支交接
		rd("dev1", "m1", 70, 15),  // 5: 第二个区间
		rd("dev1", "m2", 71, 25),  // 6
		rd("dev2", "m3", 5, 5),    // 7
		rd("dev2", "m3", 14, 5),   // 8: 与失效同刻刷新
		rd("dev2", "m1", 100, 15), // 9
	}

	run := func(batches [][]Reading, marks []int64) []Trigger {
		e := mustEngine(t, rules...)
		for i, b := range batches {
			e.Advance(b, marks[i])
		}
		return e.Triggers()
	}

	oneShot := run([][]Reading{readings}, []int64{200})

	byTime := run([][]Reading{
		{readings[0], readings[1], readings[7]},
		{readings[2], readings[3], readings[4], readings[8]},
		{readings[5], readings[6], readings[9]},
	}, []int64{7, 35, 200})

	withEmpty := run([][]Reading{
		{readings[0], readings[1], readings[7]},
		{},
		{readings[2], readings[3], readings[4], readings[8]},
		{},
		{},
		{readings[5], readings[6], readings[9]},
		{},
	}, []int64{7, 10, 35, 35, 80, 150, 200})

	shuffled := []Reading{
		readings[4], readings[0], readings[8], readings[2],
		readings[7], readings[1], readings[3], // 同批乱序
	}
	outOfOrder := run([][]Reading{
		shuffled,
		{readings[5], readings[9], readings[6]},
	}, []int64{35, 200})

	for name, got := range map[string][]Trigger{"byTime": byTime, "withEmpty": withEmpty, "outOfOrder": outOfOrder} {
		if !reflect.DeepEqual(got, oneShot) {
			t.Fatalf("%s = %v, want %v", name, got, oneShot)
		}
	}
	if len(oneShot) == 0 {
		t.Fatal("scenario produced no triggers, test is vacuous")
	}
}

// 规则校验。
func TestValidation(t *testing.T) {
	base := func() Rule {
		return Rule{
			ID:          "r1",
			DeviceIDs:   []string{"dev1"},
			Comparisons: []Comparison{cmp("a", "m1", 10, 5, 10)},
			Expression:  "a",
			Duration:    10,
			Intervals:   []Interval{{Start: 0, End: 10}},
		}
	}
	cases := map[string]func(r *Rule){
		"empty rule id":  func(r *Rule) { r.ID = "" },
		"bad duration":   func(r *Rule) { r.Duration = 0 },
		"upper <= lower": func(r *Rule) { r.Comparisons[0].Upper = 5 },
		"bad validity":   func(r *Rule) { r.Comparisons[0].Validity = 0 },
		"unknown ref":    func(r *Rule) { r.Expression = "a AND zz" },
		"bad expression": func(r *Rule) { r.Expression = "a AND" },
		"empty interval": func(r *Rule) { r.Intervals = []Interval{{Start: 5, End: 5}} },
		"overlapping":    func(r *Rule) { r.Intervals = []Interval{{0, 10}, {5, 15}} },
		"duplicate cmp": func(r *Rule) {
			r.Comparisons = append(r.Comparisons, cmp("a", "m2", 10, 5, 10))
		},
	}
	for name, mutate := range cases {
		r := base()
		mutate(&r)
		if _, err := NewEngine([]Rule{r}); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
	// 相邻区间合法
	r := base()
	r.Intervals = []Interval{{0, 10}, {10, 20}}
	if _, err := NewEngine([]Rule{r}); err != nil {
		t.Fatalf("adjacent intervals should be legal: %v", err)
	}
	// 重复规则 ID 非法
	if _, err := NewEngine([]Rule{base(), base()}); err == nil {
		t.Fatal("duplicate rule id: expected error")
	}
}

// 恰好落在相接区间边界上的读数属于后一个区间。
func TestReadingAtAdjacentBoundary(t *testing.T) {
	rule := Rule{
		ID:          "r1",
		DeviceIDs:   []string{"dev1"},
		Comparisons: []Comparison{cmp("a", "m1", 10, 5, 1000)},
		Expression:  "a",
		Duration:    5,
		Intervals:   []Interval{{Start: 0, End: 10}, {Start: 10, End: 20}},
	}
	e := mustEngine(t, rule)
	e.Advance([]Reading{rd("dev1", "m1", 10, 15)}, 100)
	checkTriggers(t, e, []Trigger{trig("r1", "dev1", 15)})
}
