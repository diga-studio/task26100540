package telemetry

import (
	"fmt"
	"sort"
)

// Engine 是规则求值引擎，持有全部规则、运行状态与触发记录。
type Engine struct {
	states   []*ruleState
	feeds    map[string]*readingFeed
	triggers []Trigger
	lastMark int64
	started  bool
}

// NewEngine 校验规则并创建引擎，规则在运行期间不修改。
func NewEngine(rules []Rule) (*Engine, error) {
	e := &Engine{feeds: map[string]*readingFeed{}}
	ruleIDs := map[string]bool{}
	for i := range rules {
		rule := &rules[i]
		if rule.ID == "" {
			return nil, fmt.Errorf("rule %d: empty rule ID", i)
		}
		if ruleIDs[rule.ID] {
			return nil, fmt.Errorf("rule %q: duplicate rule ID", rule.ID)
		}
		ruleIDs[rule.ID] = true
		if rule.Duration <= 0 {
			return nil, fmt.Errorf("rule %q: duration must be positive", rule.ID)
		}

		comps := map[string]bool{}
		for _, c := range rule.Comparisons {
			if c.Name == "" {
				return nil, fmt.Errorf("rule %q: empty comparison name", rule.ID)
			}
			if comps[c.Name] {
				return nil, fmt.Errorf("rule %q: duplicate comparison name %q", rule.ID, c.Name)
			}
			comps[c.Name] = true
			if c.Validity <= 0 {
				return nil, fmt.Errorf("rule %q comparison %q: validity must be positive", rule.ID, c.Name)
			}
			if !(c.Upper > c.Lower) {
				return nil, fmt.Errorf("rule %q comparison %q: upper threshold must be greater than lower", rule.ID, c.Name)
			}
		}

		root, err := parseExpression(rule.Expression)
		if err != nil {
			return nil, fmt.Errorf("rule %q: invalid expression: %w", rule.ID, err)
		}
		for _, name := range exprRefs(root) {
			if !comps[name] {
				return nil, fmt.Errorf("rule %q: expression references unknown comparison %q", rule.ID, name)
			}
		}

		intervals := make([]Interval, len(rule.Intervals))
		copy(intervals, rule.Intervals)
		sort.Slice(intervals, func(a, b int) bool { return intervals[a].Start < intervals[b].Start })
		for j, iv := range intervals {
			if iv.Start >= iv.End {
				return nil, fmt.Errorf("rule %q: invalid interval [%d, %d)", rule.ID, iv.Start, iv.End)
			}
			if j > 0 && iv.Start < intervals[j-1].End {
				return nil, fmt.Errorf("rule %q: overlapping intervals", rule.ID)
			}
		}

		devices := map[string]bool{}
		for _, dev := range rule.DeviceIDs {
			if devices[dev] {
				continue
			}
			devices[dev] = true
			feed := e.feeds[dev]
			if feed == nil {
				feed = &readingFeed{}
				e.feeds[dev] = feed
			}
			e.states = append(e.states, newRuleState(rule, dev, root, intervals, feed))
		}
	}
	return e, nil
}

// exprRefs 收集表达式中引用的全部比较项名字。
func exprRefs(e expr) []string {
	switch v := e.(type) {
	case refExpr:
		return []string{v.name}
	case orExpr:
		return append(exprRefs(v.left), exprRefs(v.right)...)
	case andExpr:
		return append(exprRefs(v.left), exprRefs(v.right)...)
	default:
		return nil
	}
}

// Advance 提交一批读数（同批允许乱序）并推进水位。
// 允许 readings 为空，仅推进水位。返回后截至 watermark 的触发记录完整。
func (e *Engine) Advance(readings []Reading, watermark int64) {
	for _, r := range readings {
		if feed := e.feeds[r.DeviceID]; feed != nil {
			feed.insert(r)
		}
	}
	if e.started && watermark < e.lastMark {
		watermark = e.lastMark
	}
	e.started = true
	e.lastMark = watermark
	for _, s := range e.states {
		s.run(watermark, func(tr Trigger) {
			e.triggers = append(e.triggers, tr)
		})
	}
}

// Triggers 返回截至当前水位产生的全部触发记录，按 (Time, RuleID, DeviceID) 排序。
func (e *Engine) Triggers() []Trigger {
	out := make([]Trigger, len(e.triggers))
	copy(out, e.triggers)
	sort.Slice(out, func(a, b int) bool {
		if out[a].Time != out[b].Time {
			return out[a].Time < out[b].Time
		}
		if out[a].RuleID != out[b].RuleID {
			return out[a].RuleID < out[b].RuleID
		}
		return out[a].DeviceID < out[b].DeviceID
	})
	return out
}
