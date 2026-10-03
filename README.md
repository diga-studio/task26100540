# task26100540

设备遥测规则求值库（Go）。配置、运行状态与触发记录全部保存在内存中，由上层直接调用；
业务时间完全由调用方提供，与调用频率无关。时间与持续时长均为整数毫秒。

## 用法

```go
engine, err := telemetry.NewEngine([]telemetry.Rule{{
    ID:        "overheat",
    DeviceIDs: []string{"dev1", "dev2"},
    Comparisons: []telemetry.Comparison{
        // 滞回比较：>= 上阈值置真，<= 下阈值置假，其间保持；读数有效期 60s
        {Name: "Hot", Metric: "temp", Upper: 80, Lower: 70, ValidityMs: 60_000},
        {Name: "Dry", Metric: "hum",  Upper: 20, Lower: 15, ValidityMs: 60_000},
    },
    Expression: "Hot & Dry",          // 与、或及括号组成的条件表达式
    DurationMs: 30_000,               // 持续为真 30s 才触发
    Intervals:  []telemetry.Interval{{StartMs: 0, EndMs: 3_600_000}}, // 左闭右开
}})
// 提交一批归一化读数并推进水位；同批允许乱序，可只推进水位
err = engine.Advance(readings, watermarkMs)
triggers := engine.Triggers() // 截至水位的全部触发记录，含精确触发时刻
```

## 语义要点

- 每台设备、每条规则独立求值；只记录触发，不执行设备动作。
- 比较项初始为假；读数到期未获同刻刷新则立即置假，同刻刷新不造成瞬时失效。
- 条件由假变真开始累计；同刻分支交接且表达式最终仍为真不打断累计；
  变假、读数失效或区间结束与持续期限同刻时取消触发。
- 每个生效区间开始时清空比较项与持续状态，区间外读数不影响后续区间，
  即使两个区间首尾相接也重新开始。
- 水位递增；读数时刻必须大于上一水位且不超过本次水位。
  同一组读数无论如何分批、插入空水位或一次性回放，触发记录完全一致。

## 测试

```sh
go test ./...
```
