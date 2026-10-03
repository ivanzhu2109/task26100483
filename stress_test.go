package vmplacer

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

// vms 批量生成 VM 请求：n 台 (cpu, ram) 相同的 VM，ID 带前缀编号。
func vms(prefix string, n int, cpu, ram int64) []VMRequest {
	out := make([]VMRequest, n)
	for i := range out {
		out[i] = VMRequest{ID: prefix + itoa(i), CPU: cpu, RAM: ram}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[p:])
}

func concat(groups ...[]VMRequest) []VMRequest {
	var out []VMRequest
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func het4() []Node {
	return []Node{
		{ID: "n1", Healthy: true, CPUTotal: 64, RAMTotal: 524288},
		{ID: "n2", Healthy: true, CPUTotal: 32, RAMTotal: 262144},
		{ID: "n3", Healthy: true, CPUTotal: 16, RAMTotal: 65536},
		{ID: "n4", Healthy: true, CPUTotal: 8, RAMTotal: 32768},
	}
}

func hom4(cpu, ram int64) []Node {
	return []Node{
		{ID: "h1", Healthy: true, CPUTotal: cpu, RAMTotal: ram},
		{ID: "h2", Healthy: true, CPUTotal: cpu, RAMTotal: ram},
		{ID: "h3", Healthy: true, CPUTotal: cpu, RAMTotal: ram},
		{ID: "h4", Healthy: true, CPUTotal: cpu, RAMTotal: ram},
	}
}

type stressCase struct {
	name     string
	nodes    []Node
	reqs     []VMRequest
	feasible bool
}

func stressCases() []stressCase {
	preloaded := het4()
	preloaded[0].CPUUsed, preloaded[0].RAMUsed = 45, 400000
	preloaded[1].CPUUsed, preloaded[1].RAMUsed = 22, 200000
	preloaded[2].CPUUsed, preloaded[2].RAMUsed = 11, 50000
	preloaded[3].CPUUsed, preloaded[3].RAMUsed = 5, 25000

	bigram := hom4(64, 524288)
	for i := range bigram {
		bigram[i].CPUUsed, bigram[i].RAMUsed = 10, 100000
	}

	down := het4()
	for i := range down {
		down[i].Healthy = false
	}

	return []stressCase{
		// 1. 异构宽松容量，20 台混合小 VM。
		{"het-loose", het4(), concat(
			vms("a", 5, 4, 8192), vms("b", 5, 2, 4096),
			vms("c", 5, 8, 16384), vms("d", 5, 1, 2048)), true},
		// 2. 异构 + 高预占用（约 70%），20 台小 VM，剩余空间紧张但可行。
		{"het-preloaded", preloaded, vms("p", 20, 1, 5000), true},
		// 3. 二维碎片：两维总量都恰好够，但任何节点都装不下 A+B 组合 → 不可行。
		{"frag-2d-infeasible", hom4(20, 2000), concat(
			vms("A", 10, 7, 100), vms("B", 10, 1, 700)), false},
		// 4. 与 3 同构但比例搭配可行：每节点 2A+2B+1C，CPU 用到 17/20。
		{"frag-2d-feasible", hom4(20, 2000), concat(
			vms("A", 8, 7, 100), vms("B", 8, 1, 700), vms("C", 4, 1, 50)), true},
		// 5. CPU 精确装满：每节点 1 大 4 小，CPU 利用率恰好 100%。
		{"cpu-exact-full", hom4(64, 100000), concat(
			vms("big", 4, 32, 800), vms("small", 16, 8, 25)), true},
		// 6. 与 5 差 1 核：大 VM 33 核后总需求超总容量 → 不可行。
		{"cpu-over-by-one", hom4(64, 100000), concat(
			vms("big", 4, 33, 800), vms("small", 16, 8, 25)), false},
		// 7. 单台 VM 超 80% 配额（52 > 0.8*64=51.2）→ 不可行。
		{"quota-blocked", het4(), concat(
			vms("q", 1, 52, 100), vms("r", 19, 1, 100)), false},
		// 8. RAM 精确装满：每节点 5 台，RAM 利用率恰好 100%。
		{"ram-exact-full", hom4(64, 4000), vms("m", 20, 2, 800), true},
		// 9. RAM 差 1MiB 装不下第 5 台 → 每节点 4 台，20 台放不下 → 不可行。
		{"ram-off-by-one", hom4(64, 4000), vms("m", 20, 2, 801), false},
		// 10. 全节点不健康 → 不可行。
		{"all-unhealthy", down, vms("x", 20, 1, 100), false},
		// 11. 4 个完全相同的节点 + 20 台相同 VM：对称性剪枝压力。
		{"symmetric", hom4(20, 2000), vms("s", 20, 2, 100), true},
		// 12. 大数值 RAM（接近 524288 上限）+ 预占用，近满载可行。
		{"big-ram-nearfull", bigram, concat(
			vms("huge", 4, 4, 400000), vms("tiny", 16, 2, 6000)), true},
		// 13. 最大 CPU 配额边界：51 <= 51.2 可放，每节点 1 台 + 小 VM 填充。
		{"cpu-quota-edge", hom4(64, 100000), concat(
			vms("edge", 4, 51, 100), vms("fill", 16, 3, 100)), true},
		// 14. 异构 + 混合尺寸 + 部分节点预占用：CPU 总剩余 37 核、需求 36 核，
		// 近满载可行，需精确平衡（如 n1: 4L+3M+1S, n2: 4M+2S, n3: 1M+3S, n4: 2S）。
		{"het-mixed-tight", preloaded, concat(
			vms("L", 4, 3, 15000), vms("M", 8, 2, 6000), vms("S", 8, 1, 1500)), true},
	}
}

func TestStress(t *testing.T) {
	const maxDuration = 10 * time.Second
	const maxMem = 256 << 20 // 256 MiB
	for _, c := range stressCases() {
		t.Run(c.name, func(t *testing.T) {
			if len(c.reqs) != 20 {
				t.Fatalf("stress case must have exactly 20 VMs, got %d", len(c.reqs))
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			res, err := Plan(c.nodes, c.reqs)
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			alloc := after.TotalAlloc - before.TotalAlloc

			if elapsed > maxDuration {
				t.Fatalf("too slow: %v > %v", elapsed, maxDuration)
			}
			if alloc > maxMem {
				t.Fatalf("too much memory: %d > %d", alloc, maxMem)
			}
			if c.feasible {
				if err != nil {
					t.Fatalf("expected feasible, got %v (took %v, %d B)", err, elapsed, alloc)
				}
				obj, cerr := Check(c.nodes, c.reqs, res.Placement)
				if cerr != nil {
					t.Fatalf("Check rejected Plan result: %v", cerr)
				}
				if obj != res.Objective {
					t.Fatalf("Check objective %v != Plan objective %v", obj, res.Objective)
				}
				t.Logf("feasible: U=%s (~%.4f), took %v, alloc %d B",
					res.Objective, res.Objective.Float(), elapsed, alloc)
			} else {
				if !errors.Is(err, ErrInfeasible) {
					t.Fatalf("expected ErrInfeasible, got %v", err)
				}
				t.Logf("infeasible as expected, took %v, alloc %d B", elapsed, alloc)
			}
		})
	}
}
