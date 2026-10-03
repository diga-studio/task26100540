# task26100540

设备遥测规则求值库（Go）。配置、运行状态与触发记录全部在内存中，由上层直接调用。

## 用法

```go
rules := []telemetry.Rule{
    {
        ID:        "overheat",
        DeviceIDs: []string{"dev1", "dev2"},
        Comparisons: []telemetry.Comparison{
            // 读数 >= 上阈值置真，<= 下阈值置假，其间保持；读数有效期 10s
            {Name: "hot", Metric: "temp", Upper: 80, Lower: 70, Validity: 10_000},
            {Name: "shaky", Metric: "vib", Upper: 5, Lower: 3, Validity: 10_000},
        },
        Expression: "hot AND shaky",          // 支持 AND/OR（或 &&/||）与括号
        Duration:   30_000,                    // 持续 30s 才触发
        Intervals:  []telemetry.Interval{{Start: 0, End: 86_400_000}}, // 左闭右开
    },
}
eng, err := telemetry.NewEngine(rules) // 校验规则，运行期间不修改
if err != nil { /* ... */ }

// 提交一批读数（同批允许乱序）并推进水位；允许空批仅推进水位。
// 水位语义：截至该水位的读数已齐全，之后不会补入更旧的读数。
eng.Advance([]telemetry.Reading{
    {DeviceID: "dev1", Metric: "temp", Time: 1_000, Value: 85},
}, 5_000)

triggers := eng.Triggers() // 截至当前水位的全部触发记录，按 (时间, 规则, 设备) 排序
```

## 语义要点

- 每台设备独立求值，只记录触发（`Trigger{RuleID, DeviceID, Time}`），不执行设备动作。
- 条件表达式从假变真开始累计；到达持续期限且条件仍为真时触发，记录精确触发时刻
  （无需采样恰好落在该时刻）；连续为真只触发一次，变假后取消累计并重新允许触发。
- 每个生效区间开始时清空该规则的比较项与持续状态；区间结束立即取消累计；
  区间外读数被忽略，首尾相接的区间也不继承状态。
- 同一时刻的处理顺序：先生效区间切换，再读数更新与未获刷新项失效（作为整体），
  最后判断持续期限。同刻刷新不造成瞬时失效；同刻分支交接且表达式仍为真不打断累计；
  同刻条件变假或区间结束则取消触发。
- 求值只依赖读数与水位，与调用频率无关：同一组读数无论如何合法分批、
  插入空水位推进或一次性回放，触发记录完全一致。

## 测试

```sh
go test ./...
```
