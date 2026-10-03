package telemetry

import "math"

// readingFeed 是单台设备按时间升序的读数序列，支持乱序插入。
type readingFeed struct {
	data []Reading
}

// insert 按 Time 升序插入，同刻读数保持先来先插。
func (f *readingFeed) insert(r Reading) {
	pos := 0
	lo, hi := 0, len(f.data)
	for lo < hi {
		mid := (lo + hi) / 2
		if f.data[mid].Time <= r.Time {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	pos = lo
	f.data = append(f.data, Reading{})
	copy(f.data[pos+1:], f.data[pos:])
	f.data[pos] = r
}

// itemState 是一个比较项的运行状态。
type itemState struct {
	value  bool  // 滞回比较结果
	live   bool  // 是否存在未失效的最近读数
	expiry int64 // 最近读数的失效时刻
}

// ruleState 是一条规则在一台设备上的独立求值状态。
type ruleState struct {
	ruleID    string
	deviceID  string
	duration  int64
	root      expr
	intervals []Interval // 已按 Start 升序

	comps         map[string]Comparison // 比较项名字 -> 配置
	metricToItems map[string][]string   // 指标 -> 比较项名字

	items map[string]*itemState
	vals  map[string]bool // 表达式求值用的暂存视图

	cond      bool  // 条件表达式当前值
	since     int64 // 当前连续为真的起始时刻
	triggered bool  // 当前连续为真期间是否已触发

	cur  int   // 当前生效区间下标，-1 表示在区间外
	next int   // 下一个待进入的区间下标
	now  int64 // 已处理到的时刻

	feed   *readingFeed
	cursor int // feed 中第一个未消费读数的下标
}

func newRuleState(rule *Rule, deviceID string, root expr, intervals []Interval, feed *readingFeed) *ruleState {
	items := make(map[string]*itemState, len(rule.Comparisons))
	comps := make(map[string]Comparison, len(rule.Comparisons))
	metricToItems := map[string][]string{}
	for _, c := range rule.Comparisons {
		items[c.Name] = &itemState{}
		comps[c.Name] = c
		metricToItems[c.Metric] = append(metricToItems[c.Metric], c.Name)
	}
	return &ruleState{
		ruleID:        rule.ID,
		deviceID:      deviceID,
		duration:      rule.Duration,
		root:          root,
		intervals:     intervals,
		comps:         comps,
		metricToItems: metricToItems,
		items:         items,
		vals:          make(map[string]bool, len(rule.Comparisons)),
		cur:           -1,
		now:           math.MinInt64,
		feed:          feed,
	}
}

// run 按时间顺序处理截至 watermark 的全部事件并产生触发记录。
func (s *ruleState) run(watermark int64, emit func(Trigger)) {
	for {
		// 跳过已处理时刻之前的陈旧读数（区间外读数不会被消费）。
		for s.cursor < len(s.feed.data) && s.feed.data[s.cursor].Time <= s.now {
			s.cursor++
		}
		t, ok := s.nextEvent()
		if !ok || t > watermark {
			return
		}
		s.processAt(t, emit)
	}
}

// nextEvent 计算下一个事件时刻：区间边界、下一条读数、最近失效、持续期限。
func (s *ruleState) nextEvent() (int64, bool) {
	if s.cur < 0 {
		if s.next < len(s.intervals) {
			return s.intervals[s.next].Start, true
		}
		return 0, false
	}
	t := s.intervals[s.cur].End
	if s.cursor < len(s.feed.data) {
		if rt := s.feed.data[s.cursor].Time; rt < t {
			t = rt
		}
	}
	for _, it := range s.items {
		if it.live && it.expiry < t {
			t = it.expiry
		}
	}
	if s.cond && !s.triggered {
		if tt := s.since + s.duration; tt < t {
			t = tt
		}
	}
	return t, true
}

// processAt 处理同一时刻 t 的全部事件：
// 先生效区间切换，再读数更新与未获刷新项失效（作为整体），最后判断持续期限。
func (s *ruleState) processAt(t int64, emit func(Trigger)) {
	// 1. 生效区间切换。
	if s.cur >= 0 && t == s.intervals[s.cur].End {
		s.cur = -1
		s.cond = false
		s.triggered = false
	}
	if s.cur < 0 && s.next < len(s.intervals) && s.intervals[s.next].Start == t {
		s.cur = s.next
		s.next++
		for _, it := range s.items {
			it.value = false
			it.live = false
		}
		s.cond = false
		s.triggered = false
	}
	s.now = t
	if s.cur < 0 {
		return
	}

	// 2. 读数更新与未获刷新项失效，作为一个整体。
	for s.cursor < len(s.feed.data) && s.feed.data[s.cursor].Time <= t {
		r := s.feed.data[s.cursor]
		s.cursor++
		if r.Time != t {
			continue // 区间外的陈旧读数，忽略
		}
		for _, name := range s.metricToItems[r.Metric] {
			it := s.items[name]
			c := s.comps[name]
			if r.Value >= c.Upper {
				it.value = true
			} else if r.Value <= c.Lower {
				it.value = false
			}
			it.live = true
			it.expiry = t + c.Validity
		}
	}
	for _, it := range s.items {
		if it.live && it.expiry <= t {
			it.live = false
			it.value = false
		}
	}

	// 3. 表达式求值与累计状态迁移。
	for name, it := range s.items {
		s.vals[name] = it.value
	}
	newCond := s.root.eval(s.vals)
	if newCond && !s.cond {
		s.since = t
		s.triggered = false
	}
	s.cond = newCond

	// 4. 持续期限判断。
	if s.cond && !s.triggered && s.since+s.duration <= t {
		emit(Trigger{RuleID: s.ruleID, DeviceID: s.deviceID, Time: s.since + s.duration})
		s.triggered = true
	}
}
