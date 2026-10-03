package telemetry

import "testing"

func TestReadingAtAdjacentIntervalBoundary(t *testing.T) {
	// 相邻区间交界处的读数属于后一个区间，且从空白状态开始。
	e := mustEngine(t, testRule("A", 30, []Comparison{cmpA(1000)}, []Interval{{0, 100}, {100, 200}}))
	mustAdvance(t, e, 300, rd("a", 100, 10))
	wantTriggers(t, e, 130)
}

func TestUnknownDeviceAndMetricIgnored(t *testing.T) {
	e := mustEngine(t, testRule("A", 30, []Comparison{cmpA(1000)}, []Interval{{0, 1000}}))
	mustAdvance(t, e, 100,
		Reading{DeviceID: "ghost", Metric: "a", TimeMs: 0, Value: 10},
		rd("unknown", 0, 10),
	)
	wantTriggers(t, e)
}

func TestMultipleComparisonsSameMetric(t *testing.T) {
	// 同一指标可绑定多个不同阈值的比较项，各自独立滞回。
	comps := []Comparison{cmpX("A", "a", 10, 5, 1000), cmpX("B", "a", 20, 15, 1000)}
	e := mustEngine(t, testRule("A & B", 30, comps, []Interval{{0, 1000}}))
	mustAdvance(t, e, 40, rd("a", 0, 12)) // A 真、B 假，条件为假
	wantTriggers(t, e)
	mustAdvance(t, e, 100, rd("a", 50, 20)) // B 也真，自 t=50 累计，t=80 触发
	wantTriggers(t, e, 80)
}
