// Package telemetry 提供设备遥测规则求值能力：配置、运行状态与触发记录全部保存在内存中，
// 由上层直接调用。所有时间与持续时长均为整数毫秒，业务时间完全由调用方提供。
package telemetry

// Comparison 描述一个比较项：对某个指标的读数做带滞回的阈值判断。
// 初始为假；读数 >= Upper 时置真，<= Lower 时置假，两者之间保持原状态。
// 读数自采样时刻起有效 ValidityMs 毫秒，失效后比较项立即置假。
type Comparison struct {
	Name       string  // 在条件表达式中引用的标识符，规则内唯一
	Metric     string  // 绑定的指标名
	Upper      float64 // 上阈值，必须严格大于 Lower
	Lower      float64 // 下阈值
	ValidityMs int64   // 读数有效期（毫秒），必须为正
}

// Interval 为左闭右开的生效区间 [StartMs, EndMs)。
type Interval struct {
	StartMs int64
	EndMs   int64
}

// Rule 描述一条规则及其绑定的设备。初始化后不可修改。
type Rule struct {
	ID          string
	DeviceIDs   []string     // 绑定的设备，每台设备独立求值
	Comparisons []Comparison // 比较项集合
	Expression  string       // 由比较项名、&、|、&&、||、AND、OR 及括号组成的条件表达式
	DurationMs  int64        // 持续时长（毫秒），必须为正
	Intervals   []Interval   // 互不重叠的生效区间
}

// Reading 为一条归一化读数。同一设备同一指标同一时刻至多一条。
type Reading struct {
	DeviceID string
	Metric   string
	TimeMs   int64
	Value    float64
}

// Trigger 为一条触发记录，记录精确的触发时刻。
type Trigger struct {
	RuleID   string
	DeviceID string
	TimeMs   int64
}
