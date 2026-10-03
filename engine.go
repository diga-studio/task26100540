package telemetry

import (
	"fmt"
	"sort"
)

// Engine 为内存中的规则求值引擎：配置、运行状态与触发记录全部保存在内存中。
// 业务时间完全由调用方通过 Advance 的水位参数提供，与调用频率无关。
type Engine struct {
	ruleOrder     []string
	deviceOrder   map[string][]string
	states        map[string]map[string]*ruleDeviceState // 规则ID -> 设备ID -> 状态
	triggers      []Trigger
	lastWatermark int64
	hasWatermark  bool
}

// NewEngine 校验并编译规则，为每条规则的每台绑定设备建立独立求值状态。
// 规则在初始化后不可修改。
func NewEngine(rules []Rule) (*Engine, error) {
	e := &Engine{
		deviceOrder: make(map[string][]string),
		states:      make(map[string]map[string]*ruleDeviceState),
	}
	seenRules := make(map[string]bool)
	for i := range rules {
		rule := &rules[i]
		if rule.ID == "" {
			return nil, fmt.Errorf("telemetry: 规则缺少 ID")
		}
		if seenRules[rule.ID] {
			return nil, fmt.Errorf("telemetry: 规则 ID %q 重复", rule.ID)
		}
		seenRules[rule.ID] = true
		cr, err := compileRule(rule)
		if err != nil {
			return nil, fmt.Errorf("telemetry: 规则 %q 无效: %w", rule.ID, err)
		}
		devices, err := checkDevices(rule.ID, rule.DeviceIDs)
		if err != nil {
			return nil, err
		}
		e.ruleOrder = append(e.ruleOrder, rule.ID)
		e.deviceOrder[rule.ID] = devices
		e.states[rule.ID] = make(map[string]*ruleDeviceState)
		for _, deviceID := range devices {
			e.states[rule.ID][deviceID] = newRuleDeviceState(cr, deviceID)
		}
	}
	return e, nil
}

func compileRule(rule *Rule) (*compiledRule, error) {
	if len(rule.Comparisons) == 0 {
		return nil, fmt.Errorf("至少需要一个比较项")
	}
	if rule.DurationMs <= 0 {
		return nil, fmt.Errorf("持续时长必须为正")
	}
	refs := make(map[string]int, len(rule.Comparisons))
	compByMetric := make(map[string][]int)
	for i, c := range rule.Comparisons {
		if c.Name == "" {
			return nil, fmt.Errorf("比较项缺少名称")
		}
		if _, dup := refs[c.Name]; dup {
			return nil, fmt.Errorf("比较项 %q 重复", c.Name)
		}
		if !(c.Upper > c.Lower) {
			return nil, fmt.Errorf("比较项 %q 的上阈值必须严格大于下阈值", c.Name)
		}
		if c.ValidityMs <= 0 {
			return nil, fmt.Errorf("比较项 %q 的读数有效期必须为正", c.Name)
		}
		refs[c.Name] = i
		compByMetric[c.Metric] = append(compByMetric[c.Metric], i)
	}
	expr, err := parseExpr(rule.Expression, refs)
	if err != nil {
		return nil, err
	}
	intervals, err := checkIntervals(rule.Intervals)
	if err != nil {
		return nil, err
	}
	return &compiledRule{
		id:           rule.ID,
		comps:        append([]Comparison(nil), rule.Comparisons...),
		compByMetric: compByMetric,
		expr:         expr,
		durationMs:   rule.DurationMs,
		intervals:    intervals,
	}, nil
}

func checkDevices(ruleID string, deviceIDs []string) ([]string, error) {
	seen := make(map[string]bool, len(deviceIDs))
	devices := make([]string, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		if id == "" {
			return nil, fmt.Errorf("telemetry: 规则 %q 绑定了空设备 ID", ruleID)
		}
		if seen[id] {
			return nil, fmt.Errorf("telemetry: 规则 %q 重复绑定设备 %q", ruleID, id)
		}
		seen[id] = true
		devices = append(devices, id)
	}
	return devices, nil
}

// checkIntervals 校验生效区间互不重叠，返回按开始时刻排序的副本。
func checkIntervals(intervals []Interval) ([]Interval, error) {
	sorted := append([]Interval(nil), intervals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartMs < sorted[j].StartMs })
	for i, iv := range sorted {
		if iv.StartMs >= iv.EndMs {
			return nil, fmt.Errorf("生效区间 [%d, %d) 无效", iv.StartMs, iv.EndMs)
		}
		if i > 0 && iv.StartMs < sorted[i-1].EndMs {
			return nil, fmt.Errorf("生效区间 [%d, %d) 与 [%d, %d) 重叠",
				sorted[i-1].StartMs, sorted[i-1].EndMs, iv.StartMs, iv.EndMs)
		}
	}
	return sorted, nil
}

// Advance 提交一批归一化读数并推进水位。同批读数允许乱序，可以不提交读数仅推进水位。
// 水位必须递增；读数时刻必须大于上一水位且不超过本次水位。
// 返回后，截至水位的触发记录完整，与分批方式无关。
func (e *Engine) Advance(readings []Reading, watermarkMs int64) error {
	if e.hasWatermark && watermarkMs < e.lastWatermark {
		return fmt.Errorf("telemetry: 水位 %d 小于当前水位 %d", watermarkMs, e.lastWatermark)
	}
	for _, r := range readings {
		if e.hasWatermark && r.TimeMs <= e.lastWatermark {
			return fmt.Errorf("telemetry: 设备 %q 指标 %q 的读数时刻 %d 不晚于当前水位 %d",
				r.DeviceID, r.Metric, r.TimeMs, e.lastWatermark)
		}
		if r.TimeMs > watermarkMs {
			return fmt.Errorf("telemetry: 设备 %q 指标 %q 的读数时刻 %d 超过水位 %d",
				r.DeviceID, r.Metric, r.TimeMs, watermarkMs)
		}
	}
	byDevice := make(map[string][]Reading)
	for _, r := range readings {
		byDevice[r.DeviceID] = append(byDevice[r.DeviceID], r)
	}
	for _, list := range byDevice {
		sort.SliceStable(list, func(i, j int) bool { return list[i].TimeMs < list[j].TimeMs })
	}
	for _, ruleID := range e.ruleOrder {
		for _, deviceID := range e.deviceOrder[ruleID] {
			st := e.states[ruleID][deviceID]
			st.advance(byDevice[deviceID], watermarkMs, &e.triggers)
		}
	}
	e.lastWatermark = watermarkMs
	e.hasWatermark = true
	return nil
}

// Triggers 返回截至当前水位的全部触发记录，按 (时刻, 规则, 设备) 排序。
func (e *Engine) Triggers() []Trigger {
	out := make([]Trigger, len(e.triggers))
	copy(out, e.triggers)
	sort.Slice(out, func(i, j int) bool {
		if out[i].TimeMs != out[j].TimeMs {
			return out[i].TimeMs < out[j].TimeMs
		}
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		return out[i].DeviceID < out[j].DeviceID
	})
	return out
}
