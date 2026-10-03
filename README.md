# vmplacer — 边缘集群批量 VM 放置规划器

在固定快照上为整批 VM 求解放置方案：**整批全部放得下才接纳**；可接纳时返回
**热点利用率最低**（最小化 `U = max_{健康节点} max(CPU利用率, RAM利用率)`）的方案。
目标值用精确比例（`Ratio{Num, Den}`，int64 交叉相乘比较）表示与比较，不经过浮点。
只做规划，不执行 VM 操作，不涉及持久化与并发提交。

## 库用法

```go
nodes := []vmplacer.Node{
    {ID: "n1", Healthy: true, CPUTotal: 16, RAMTotal: 65536, CPUUsed: 4, RAMUsed: 16384},
    {ID: "n2", Healthy: true, CPUTotal: 8, RAMTotal: 32768},
}
reqs := []vmplacer.VMRequest{
    {ID: "vm1", CPU: 4, RAM: 8192},
    {ID: "vm2", CPU: 2, RAM: 4096},
}

res, err := vmplacer.Plan(nodes, reqs)
if errors.Is(err, vmplacer.ErrInfeasible) {
    // 整批无可行放置
} else if err != nil {
    // 输入不合法
} else {
    // res.Placement: map[vmID]nodeID，每台 VM 恰好一台节点
    // res.Objective: 精确比例，如 3/4
}

// 校验任何（含调用者自行构造的）方案：完整性、80% 配额、节点容量，
// 并返回同一定义的精确目标值；不判断最优性。
obj, err := vmplacer.Check(nodes, reqs, res.Placement)
```

## 放置规则

- 单台 VM 的 CPU/RAM 分别不得超过目标节点**总容量**的 80%（等号允许）；
  与剩余容量无关，但节点原有占用 + 新分配不得超过总容量（不超卖、不拆分）。
- 只有健康节点可接纳新 VM；现有占用固定不动。
- 输入约束：2–4 节点、1–20 台 VM、CPU ≤ 64 核/节点、RAM ≤ 524288 MiB/节点。

## 求解方法

仓库内实现的分支定界精确搜索（`solve.go`）：VM 按需求难度降序展开，
节点按放置后利用率升序分支；剪枝包括容量/配额检查、前向可行性、
当前部分解下界（含按容量加权的聚合负载下界）与相同状态节点的对称消除。
找到的可行解即全局最优，不存在近似、超时降级或外部求解器。

## 测试

```sh
go test ./...                  # 全部：单元 + 穷举对照 + 压力
go test -run TestPlanMatchesExhaustiveReference -v   # 140 组固定种子随机实例 vs 独立穷举参考
go test -run TestStress -v                           # 14 组 4 节点 / 20 VM 压力样例（含耗时与内存记录）
```

- `plan_test.go`：贪心陷阱、80% 配额等号/越界、配额按总容量而非剩余容量、
  不健康节点、Check 校验、精确目标值。
- `fuzz_ref_test.go`：独立穷举参考实现对照可行性与精确最优值；含
  “总量充足、单台可放、整批不可行”实例（手工构造 + 随机套件中验证存在）。
- `stress_test.go`：异构容量、混合尺寸、近满载可行、总量满足但不可行、
  对称节点、大数值 RAM 等 14 组固定样例；每例断言 < 10s、额外内存 < 256 MiB，
  并用 `Check` 核验所有返回方案。
