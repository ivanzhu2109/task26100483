package vmplacer

import (
	"math/rand"
	"testing"
)

// 独立穷举参考实现：不复用求解器的任何搜索逻辑，
// 直接枚举所有 VM→健康节点 的指派，验证配额与容量，取精确最小目标值。
func refSolve(nodes []Node, reqs []VMRequest) (Ratio, bool) {
	var healthy []int
	for i, n := range nodes {
		if n.Healthy {
			healthy = append(healthy, i)
		}
	}
	if len(healthy) == 0 {
		return Ratio{}, false
	}
	cpu := make([]int64, len(nodes))
	ram := make([]int64, len(nodes))
	for i, n := range nodes {
		cpu[i] = n.CPUUsed
		ram[i] = n.RAMUsed
	}
	best := Ratio{}
	found := false
	var rec func(k int)
	rec = func(k int) {
		if k == len(reqs) {
			obj, err := objective(nodes, cpu, ram)
			if err != nil {
				return
			}
			if !found || obj.Cmp(best) < 0 {
				best, found = obj, true
			}
			return
		}
		r := reqs[k]
		for _, i := range healthy {
			n := nodes[i]
			if r.CPU*5 > 4*n.CPUTotal || r.RAM*5 > 4*n.RAMTotal {
				continue
			}
			if cpu[i]+r.CPU > n.CPUTotal || ram[i]+r.RAM > n.RAMTotal {
				continue
			}
			cpu[i] += r.CPU
			ram[i] += r.RAM
			rec(k + 1)
			cpu[i] -= r.CPU
			ram[i] -= r.RAM
		}
	}
	rec(0)
	return best, found
}

// 性质 P：两维总剩余都充足、每台 VM 单独都能放下，但整批不可行。
func hasPropertyP(nodes []Node, reqs []VMRequest, feasible bool) bool {
	if feasible {
		return false
	}
	var remCPU, remRAM, demCPU, demRAM int64
	for _, n := range nodes {
		if n.Healthy {
			remCPU += n.CPUTotal - n.CPUUsed
			remRAM += n.RAMTotal - n.RAMUsed
		}
	}
	for _, r := range reqs {
		demCPU += r.CPU
		demRAM += r.RAM
		ok := false
		for _, n := range nodes {
			if n.Healthy && fitsQuota(r, n) &&
				r.CPU <= n.CPUTotal-n.CPUUsed && r.RAM <= n.RAMTotal-n.RAMUsed {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return demCPU <= remCPU && demRAM <= remRAM
}

// 手工构造的性质 P 实例：3 台 6 核 VM，2 个 10 核节点各只能装 1 台。
func TestPropertyPConstructed(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, CPUTotal: 10, RAMTotal: 4096},
		{ID: "b", Healthy: true, CPUTotal: 10, RAMTotal: 4096},
	}
	reqs := []VMRequest{
		{ID: "v1", CPU: 6, RAM: 100},
		{ID: "v2", CPU: 6, RAM: 100},
		{ID: "v3", CPU: 6, RAM: 100},
	}
	if _, err := Plan(nodes, reqs); err != ErrInfeasible {
		t.Fatalf("expected ErrInfeasible, got %v", err)
	}
	_, feasible := refSolve(nodes, reqs)
	if !hasPropertyP(nodes, reqs, feasible) {
		t.Fatal("constructed instance must satisfy property P")
	}
}

// 固定种子随机小实例：与穷举参考对照可行性与精确最优值。
func TestPlanMatchesExhaustiveReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20261003))
	const total = 140
	pCount := 0
	for tc := 0; tc < total; tc++ {
		nNodes := 2 + rng.Intn(3) // 2..4
		nodes := make([]Node, nNodes)
		for i := range nodes {
			cpuTot := int64(4 + rng.Intn(61))           // 4..64
			ramTot := int64(512 + rng.Intn(524288-512)) // 512..524288
			n := Node{
				ID:       string(rune('a' + i)),
				Healthy:  rng.Intn(5) > 0, // 1/5 概率不健康
				CPUTotal: cpuTot,
				RAMTotal: ramTot,
			}
			// 已有占用不超过容量。
			n.CPUUsed = int64(rng.Intn(int(n.CPUTotal) + 1))
			n.RAMUsed = int64(rng.Intn(int(n.RAMTotal) + 1))
			nodes[i] = n
		}
		nVMs := 1 + rng.Intn(7) // 1..7
		reqs := make([]VMRequest, nVMs)
		for j := range reqs {
			// 需求基准取最小健康节点容量，保证相当比例的实例接近容量边界。
			var minCPU, minRAM int64 = MaxCPU, MaxRAM
			for _, n := range nodes {
				if n.Healthy {
					if n.CPUTotal < minCPU {
						minCPU = n.CPUTotal
					}
					if n.RAMTotal < minRAM {
						minRAM = n.RAMTotal
					}
				}
			}
			cpu := int64(1 + rng.Intn(int(minCPU*8/10)))
			ram := int64(1 + rng.Intn(int(minRAM*8/10)))
			reqs[j] = VMRequest{ID: string(rune('A' + j)), CPU: cpu, RAM: ram}
		}

		res, err := Plan(nodes, reqs)
		refObj, refOK := refSolve(nodes, reqs)
		if refOK != (err == nil) {
			t.Fatalf("case %d: feasibility mismatch: Plan err=%v, ref feasible=%v\nnodes=%+v\nreqs=%+v",
				tc, err, refOK, nodes, reqs)
		}
		if refOK {
			if res.Objective != refObj {
				t.Fatalf("case %d: objective mismatch: Plan=%v ref=%v\nnodes=%+v\nreqs=%+v",
					tc, res.Objective, refObj, nodes, reqs)
			}
			// 用 Check 独立核验 Plan 返回的方案与目标值。
			obj, cerr := Check(nodes, reqs, res.Placement)
			if cerr != nil {
				t.Fatalf("case %d: Check rejected Plan result: %v", tc, cerr)
			}
			if obj != res.Objective {
				t.Fatalf("case %d: Check objective %v != Plan objective %v", tc, obj, res.Objective)
			}
		}
		if hasPropertyP(nodes, reqs, refOK) {
			pCount++
		}
	}
	if pCount == 0 {
		t.Fatal("random suite must contain at least one property-P instance")
	}
	t.Logf("property-P instances in suite: %d/%d", pCount, total)
}
