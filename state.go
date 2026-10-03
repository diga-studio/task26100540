package telemetry

import "math"

// compiledRule 为校验并编译后的规则。
type compiledRule struct {
	id           string
	comps        []Comparison
	compByMetric map[string][]int // 指标名 -> 比较项下标
	expr         exprNode
	durationMs   int64
	intervals    []Interval // 已按开始时刻排序且互不重叠
}

// compState 为单个比较项在一台设备上的运行状态。
type compState struct {
	value      bool
	hasReading bool
	expiryMs   int64
}

// ruleDeviceState 为 (规则, 设备) 维度的独立求值状态。
type ruleDeviceState struct {
	rule         *compiledRule
	deviceID     string
	comps        []compState
	values       []bool // 表达式求值用的复用缓冲
	condTrue     bool
	trueSinceMs  int64
	triggered    bool
	inInterval   bool
	intervalEnd  int64
	nextInterval int
}

func newRuleDeviceState(rule *compiledRule, deviceID string) *ruleDeviceState {
	return &ruleDeviceState{
		rule:     rule,
		deviceID: deviceID,
		comps:    make([]compState, len(rule.comps)),
		values:   make([]bool, len(rule.comps)),
	}
}

// clear 清空比较项状态与持续状态（区间开始/结束时调用）。
func (st *ruleDeviceState) clear() {
	for i := range st.comps {
		st.comps[i] = compState{}
	}
	st.condTrue = false
	st.triggered = false
}

// advance 处理按时刻排序的读数（均大于上一水位且 <= watermarkMs），
// 并推进所有时间事件（读数失效、区间切换、持续期限）至水位，触发的记录追加到 out。
// 同一时刻的处理顺序：先生效区间切换，再读数更新与未获刷新项的失效（作为整体），
// 最后重算条件并判断持续期限。
func (st *ruleDeviceState) advance(readings []Reading, watermarkMs int64, out *[]Trigger) {
	ri := 0
	for {
		t := st.nextEventTime(readings, ri)
		if t > watermarkMs {
			return
		}
		st.applyIntervalBounds(t)
		ri = st.applyReadings(readings, ri, t)
		st.applyExpiries(t)
		st.updateCondition(t)
		st.checkTrigger(t, out)
	}
}

// nextEventTime 计算下一个待处理事件的时刻：下一条读数、最近的读数失效、
// 区间边界（当前区间结束或下一区间开始）、持续期限到达时刻。
func (st *ruleDeviceState) nextEventTime(readings []Reading, ri int) int64 {
	t := int64(math.MaxInt64)
	if ri < len(readings) && readings[ri].TimeMs < t {
		t = readings[ri].TimeMs
	}
	for i := range st.comps {
		if st.comps[i].hasReading && st.comps[i].expiryMs < t {
			t = st.comps[i].expiryMs
		}
	}
	if st.inInterval && st.intervalEnd < t {
		t = st.intervalEnd
	}
	if st.nextInterval < len(st.rule.intervals) {
		if s := st.rule.intervals[st.nextInterval].StartMs; s < t {
			t = s
		}
	}
	if st.condTrue && !st.triggered {
		if tt := st.trueSinceMs + st.rule.durationMs; tt < t {
			t = tt
		}
	}
	return t
}

// applyIntervalBounds 处理时刻 t 的区间切换：先结束当前区间（取消累计并清空状态），
// 再开始相邻的下一区间（同样从空白状态重新开始）。
func (st *ruleDeviceState) applyIntervalBounds(t int64) {
	if st.inInterval && st.intervalEnd == t {
		st.inInterval = false
		st.clear()
	}
	if st.nextInterval < len(st.rule.intervals) && st.rule.intervals[st.nextInterval].StartMs == t {
		iv := st.rule.intervals[st.nextInterval]
		st.nextInterval++
		st.inInterval = true
		st.intervalEnd = iv.EndMs
		st.clear()
	}
}

// applyReadings 应用时刻 t 的全部读数，返回新的读数游标。
// 区间外的读数被忽略（仍消耗游标）。读数按滞回规则更新比较项，并刷新其有效期。
func (st *ruleDeviceState) applyReadings(readings []Reading, ri int, t int64) int {
	for ri < len(readings) && readings[ri].TimeMs == t {
		if st.inInterval {
			r := readings[ri]
			for _, ci := range st.rule.compByMetric[r.Metric] {
				spec := st.rule.comps[ci]
				c := &st.comps[ci]
				switch {
				case r.Value >= spec.Upper:
					c.value = true
				case r.Value <= spec.Lower:
					c.value = false
				}
				c.hasReading = true
				c.expiryMs = t + spec.ValidityMs
			}
		}
		ri++
	}
	return ri
}

// applyExpiries 使时刻 t 到期且未获刷新的比较项立即置假。
// 同刻已刷新的比较项有效期已被延长，不会在此刻到期。
func (st *ruleDeviceState) applyExpiries(t int64) {
	for i := range st.comps {
		if st.comps[i].hasReading && st.comps[i].expiryMs == t {
			st.comps[i].value = false
			st.comps[i].hasReading = false
		}
	}
}

// updateCondition 重算条件表达式。由假变真时开始累计；持续为真（包括同刻分支交接）
// 不打断累计；变假则取消累计并重新允许触发。
func (st *ruleDeviceState) updateCondition(t int64) {
	cond := false
	if st.inInterval {
		for i := range st.comps {
			st.values[i] = st.comps[i].value
		}
		cond = st.rule.expr.eval(st.values)
	}
	if cond {
		if !st.condTrue {
			st.trueSinceMs = t
			st.triggered = false
		}
		st.condTrue = true
	} else {
		st.condTrue = false
		st.triggered = false
	}
}

// checkTrigger 在条件持续为真且到达持续期限时产生一条触发记录，
// 记录精确触发时刻 trueSinceMs + durationMs。连续为真期间只触发一次。
func (st *ruleDeviceState) checkTrigger(t int64, out *[]Trigger) {
	if st.condTrue && !st.triggered && st.trueSinceMs+st.rule.durationMs <= t {
		*out = append(*out, Trigger{
			RuleID:   st.rule.id,
			DeviceID: st.deviceID,
			TimeMs:   st.trueSinceMs + st.rule.durationMs,
		})
		st.triggered = true
	}
}
