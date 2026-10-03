package vmplacer

import (
	"errors"
	"testing"
)

// 贪心陷阱：先放一台可去多个节点的 VM，若放错节点会挡住后续只能去特定节点的 VM，
// 但整批实际可行。精确求解必须找到可行解。
func TestPlanGreedyTrap(t *testing.T) {
	nodes := []Node{
		{ID: "big", Healthy: true, CPUTotal: 10, RAMTotal: 1000},
		{ID: "small", Healthy: true, CPUTotal: 8, RAMTotal: 1000},
	}
	reqs := []VMRequest{
		{ID: "flex", CPU: 5, RAM: 100},  // 两个节点都能去
		{ID: "stuck", CPU: 6, RAM: 100}, // 只能去 big（6 > 8*0.8=6.4? 否，6<=6.4 可去 small）
	}
	_ = reqs
	// 重新构造：stuck 需求 7 核，超过 small 的 80%（6.4），只能去 big；
	// 若 flex 先占了 big 的 5 核，big 剩 5 核 < 7，stuck 无处可去 → 该贪心失败。
	// 但 flex 去 small、stuck 去 big 是可行的。
	reqs = []VMRequest{
		{ID: "flex", CPU: 5, RAM: 100},
		{ID: "stuck", CPU: 7, RAM: 100},
	}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatalf("expected feasible, got %v", err)
	}
	if res.Placement["stuck"] != "big" || res.Placement["flex"] != "small" {
		t.Fatalf("unexpected placement: %v", res.Placement)
	}
	if _, err := Check(nodes, reqs, res.Placement); err != nil {
		t.Fatalf("Check rejected Plan result: %v", err)
	}
}

// 80% 配额的等号与刚刚越界。
func TestQuotaBoundary(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, CPUTotal: 10, RAMTotal: 1000},
		{ID: "b", Healthy: true, CPUTotal: 10, RAMTotal: 1000},
	}
	// 等号：8 = 0.8*10，800 = 0.8*1000，必须允许。
	eq := []VMRequest{{ID: "eq", CPU: 8, RAM: 800}}
	if _, err := Plan(nodes, eq); err != nil {
		t.Fatalf("exact 80%% must be allowed, got %v", err)
	}
	// 刚刚越界：CPU 9 > 8，或 RAM 801 > 800，两台节点都一样 → 无解。
	for _, r := range [][]VMRequest{
		{{ID: "over-cpu", CPU: 9, RAM: 100}},
		{{ID: "over-ram", CPU: 1, RAM: 801}},
	} {
		if _, err := Plan(nodes, r); !errors.Is(err, ErrInfeasible) {
			t.Fatalf("expected ErrInfeasible for %v, got %v", r, err)
		}
		if _, err := Check(nodes, r, map[string]string{r[0].ID: "a"}); err == nil {
			t.Fatalf("Check must reject quota violation for %v", r)
		}
	}
}

// 配额是相对节点总容量的 80%，不是剩余容量的 80%。
// 若误算成剩余容量比例，本例会被错误判为不可行。
func TestQuotaIsOfTotalNotRemaining(t *testing.T) {
	nodes := []Node{
		// 总量 10 核，已占 3 核，剩 7 核。VM 要 6 核：
		// 6 <= 0.8*10=8（允许），但 6 > 0.8*7=5.6（误判会拒绝）。
		{ID: "a", Healthy: true, CPUTotal: 10, RAMTotal: 1000, CPUUsed: 3, RAMUsed: 100},
		{ID: "b", Healthy: true, CPUTotal: 10, RAMTotal: 1000, CPUUsed: 9, RAMUsed: 900},
	}
	reqs := []VMRequest{{ID: "vm", CPU: 6, RAM: 100}}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatalf("quota must be vs total capacity, got %v", err)
	}
	if res.Placement["vm"] != "a" {
		t.Fatalf("vm must go to node a (b has no room), got %v", res.Placement)
	}
}

// 配额按总容量算允许，但剩余容量不足时仍不可放。
func TestRemainingCapacityStillEnforced(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, CPUTotal: 10, RAMTotal: 1000, CPUUsed: 3, RAMUsed: 0},
		{ID: "b", Healthy: true, CPUTotal: 10, RAMTotal: 1000, CPUUsed: 0, RAMUsed: 0},
	}
	// 8 核满足配额（<=8），但 a 只剩 7 核；b 空闲可放。整批可行且必须落 b。
	reqs := []VMRequest{{ID: "vm", CPU: 8, RAM: 100}}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.Placement["vm"] != "b" {
		t.Fatalf("expected b, got %v", res.Placement)
	}
	// 两台都占 3 核时，8 核 VM 无处可去（配额允许但剩余不足）→ 无解。
	nodes[1].CPUUsed = 3
	if _, err := Plan(nodes, reqs); !errors.Is(err, ErrInfeasible) {
		t.Fatalf("expected ErrInfeasible, got %v", err)
	}
}

// 不健康节点不能接纳新 VM，也不计入目标值。
func TestUnhealthyNodes(t *testing.T) {
	nodes := []Node{
		{ID: "down", Healthy: false, CPUTotal: 64, RAMTotal: 524288},
		{ID: "up", Healthy: true, CPUTotal: 10, RAMTotal: 1000},
	}
	reqs := []VMRequest{{ID: "vm", CPU: 9, RAM: 100}} // 只有 down 装得下配额
	if _, err := Plan(nodes, reqs); !errors.Is(err, ErrInfeasible) {
		t.Fatalf("unhealthy node must not accept VMs, got %v", err)
	}
	if _, err := Check(nodes, []VMRequest{{ID: "x", CPU: 1, RAM: 1}}, map[string]string{"x": "down"}); err == nil {
		t.Fatal("Check must reject assignment to unhealthy node")
	}
	// 全部不健康 → 无解。
	nodes[1].Healthy = false
	if _, err := Plan(nodes, []VMRequest{{ID: "v", CPU: 1, RAM: 1}}); !errors.Is(err, ErrInfeasible) {
		t.Fatalf("expected ErrInfeasible, got %v", err)
	}
}

// Check 拒绝不完整/非法方案，并接受调用者自行构造的合法方案。
func TestCheckValidation(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, CPUTotal: 10, RAMTotal: 1000},
		{ID: "b", Healthy: true, CPUTotal: 10, RAMTotal: 1000},
	}
	reqs := []VMRequest{{ID: "v1", CPU: 2, RAM: 100}, {ID: "v2", CPU: 3, RAM: 200}}
	cases := []struct {
		name string
		p    map[string]string
	}{
		{"missing vm", map[string]string{"v1": "a"}},
		{"unknown vm", map[string]string{"v1": "a", "v2": "b", "v3": "a"}},
		{"unknown node", map[string]string{"v1": "a", "v2": "zzz"}},
		{"overcommit", map[string]string{"v1": "a", "v2": "a"}}, // RAM 够 CPU 够？2+3<=10 不超
	}
	for _, c := range cases[:3] {
		if _, err := Check(nodes, reqs, c.p); err == nil {
			t.Fatalf("%s: expected error", c.name)
		}
	}
	// 合法但非最优的方案也要能通过 Check，并给出精确目标值。
	obj, err := Check(nodes, reqs, map[string]string{"v1": "a", "v2": "a"})
	if err != nil {
		t.Fatalf("valid suboptimal placement must pass Check: %v", err)
	}
	// a: cpu 5/10=1/2, ram 300/1000=3/10 → U=1/2
	if obj != (Ratio{Num: 1, Den: 2}) {
		t.Fatalf("objective = %v, want 1/2", obj)
	}
	// 超配检查：两台 VM 都塞 a 的 CPU 不超，改成超配场景。
	big := []VMRequest{{ID: "w1", CPU: 6, RAM: 10}, {ID: "w2", CPU: 6, RAM: 10}}
	if _, err := Check(nodes, big, map[string]string{"w1": "a", "w2": "a"}); err == nil {
		t.Fatal("Check must reject node overcommit")
	}
}

// 目标值精确性：1/3 与 333/1000 之类浮点易混的比例必须严格区分。
func TestExactObjective(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, CPUTotal: 3, RAMTotal: 1000},
		{ID: "b", Healthy: true, CPUTotal: 3, RAMTotal: 1000},
	}
	// 最优是把两台 1 核 VM 分到不同节点：U = 1/3。
	reqs := []VMRequest{{ID: "v1", CPU: 1, RAM: 1}, {ID: "v2", CPU: 1, RAM: 1}}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if res.Objective != (Ratio{Num: 1, Den: 3}) {
		t.Fatalf("objective = %v, want 1/3", res.Objective)
	}
}
