// Package telemetry 提供设备遥测规则求值能力。
// 配置、运行状态与触发记录全部保存在内存中，由上层直接调用。
package telemetry

// Reading 是一条归一化读数，时间单位为毫秒。
type Reading struct {
	DeviceID string
	Metric   string
	Time     int64
	Value    float64
}

// Comparison 是一个比较项：带上下阈值的滞回比较。
// 读数 >= Upper 置真，<= Lower 置假，两者之间保持原状态。
type Comparison struct {
	Name     string  // 在条件表达式中引用的名字
	Metric   string  // 绑定的指标
	Upper    float64 // 上阈值，必须严格大于 Lower
	Lower    float64 // 下阈值
	Validity int64   // 读数有效期（毫秒），必须为正
}

// Interval 是生效区间，左闭右开 [Start, End)。
type Interval struct {
	Start int64
	End   int64
}

// Rule 是一条求值规则，初始化后不再修改。
type Rule struct {
	ID          string
	DeviceIDs   []string // 绑定的设备
	Comparisons []Comparison
	Expression  string     // 由比较项名字、AND/OR 及括号组成的条件表达式
	Duration    int64      // 持续时长（毫秒），必须为正
	Intervals   []Interval // 互不重叠的生效区间
}

// Trigger 是一条触发记录，Time 为精确触发时刻。
type Trigger struct {
	RuleID   string
	DeviceID string
	Time     int64
}
