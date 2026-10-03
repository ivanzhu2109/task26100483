package vmplanner

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

type stressCase struct {
	name  string
	nodes []Node
	reqs  []Request
	want  bool // expected feasibility
}

func vm(id string, cpu, ram int64) Request {
	return Request{ID: id, CPU: cpu, RAM: ram}
}

func vms(prefix string, n int, cpu, ram int64) []Request {
	out := make([]Request, n)
	for i := range out {
		out[i] = vm(fmt.Sprintf("%s-%02d", prefix, i+1), cpu, ram)
	}
	return out
}

func concat(batches ...[]Request) []Request {
	var out []Request
	for _, b := range batches {
		out = append(out, b...)
	}
	return out
}

// buildStressCases returns fixed stress samples: 4 nodes and 20 VMs each,
// covering heterogeneous capacities, mixed VM sizes, near-full feasible
// batches, and aggregate-sufficient-but-infeasible batches.
func buildStressCases() []stressCase {
	var cases []stressCase

	// 1. Heterogeneous capacities, mixed VM sizes, comfortable load.
	cases = append(cases, stressCase{
		name: "hetero-mixed-comfortable",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
			{ID: "n2", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
			{ID: "n3", Healthy: true, TotalCPU: 24, TotalRAM: 32768},
			{ID: "n4", Healthy: true, TotalCPU: 32, TotalRAM: 65536},
		},
		reqs: concat(
			vms("tiny", 8, 1, 512),
			vms("small", 6, 2, 2048),
			vms("mid", 4, 4, 4096),
			vms("big", 2, 6, 8192),
		),
		want: true,
	})

	// 2. Near-full feasible: every node lands at 16382/16384 RAM (~99.99%).
	cases = append(cases, stressCase{
		name: "near-full-feasible-ram",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 16, TotalRAM: 16384, UsedCPU: 8, UsedRAM: 8192},
			{ID: "n2", Healthy: true, TotalCPU: 16, TotalRAM: 16384, UsedCPU: 8, UsedRAM: 8192},
			{ID: "n3", Healthy: true, TotalCPU: 16, TotalRAM: 16384, UsedCPU: 8, UsedRAM: 8192},
			{ID: "n4", Healthy: true, TotalCPU: 16, TotalRAM: 16384, UsedCPU: 8, UsedRAM: 8192},
		},
		reqs: vms("vm", 20, 1, 1638),
		want: true,
	})

	// 3. Near-full feasible and perfectly balanced: U = 15/16 exactly.
	cases = append(cases, stressCase{
		name: "near-full-feasible-balanced",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
			{ID: "n2", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
			{ID: "n3", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
			{ID: "n4", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
		},
		reqs: vms("vm", 20, 3, 3072),
		want: true,
	})

	// 4. Aggregate-sufficient but infeasible: 5 big VMs, each needs its own
	// node (8+8 > 14 CPU), but only 4 nodes exist. 55/56 CPU used in total.
	cases = append(cases, stressCase{
		name: "aggregate-sufficient-infeasible-pigeonhole",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 14, TotalRAM: 2048},
			{ID: "n2", Healthy: true, TotalCPU: 14, TotalRAM: 2048},
			{ID: "n3", Healthy: true, TotalCPU: 14, TotalRAM: 2048},
			{ID: "n4", Healthy: true, TotalCPU: 14, TotalRAM: 2048},
		},
		reqs: concat(
			vms("big", 5, 8, 256),
			vms("tiny", 15, 1, 64),
		),
		want: false,
	})

	// 5. Aggregate-sufficient but infeasible via RAM fragmentation.
	cases = append(cases, stressCase{
		name: "aggregate-sufficient-infeasible-ram-frag",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 16, TotalRAM: 4096},
			{ID: "n2", Healthy: true, TotalCPU: 16, TotalRAM: 4096},
			{ID: "n3", Healthy: true, TotalCPU: 16, TotalRAM: 4096},
			{ID: "n4", Healthy: true, TotalCPU: 16, TotalRAM: 4096},
		},
		reqs: concat(
			vms("ramhog", 5, 2, 2500), // one per node (2*2500 > 4096)
			vms("tiny", 15, 1, 100),
		),
		want: false,
	})

	// 6. One unhealthy high-capacity node; batch fits on the healthy three.
	cases = append(cases, stressCase{
		name: "unhealthy-node-excluded-feasible",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
			{ID: "n2", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
			{ID: "n3", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
			{ID: "n4", Healthy: false, TotalCPU: 64, TotalRAM: 524288},
		},
		reqs: vms("vm", 20, 1, 1024),
		want: true,
	})

	// 7. Same nodes, but the batch only fits if the unhealthy node counted.
	cases = append(cases, stressCase{
		name: "unhealthy-node-would-be-needed",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
			{ID: "n2", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
			{ID: "n3", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
			{ID: "n4", Healthy: false, TotalCPU: 64, TotalRAM: 524288},
		},
		reqs: vms("vm", 20, 2, 512),
		want: false,
	})

	// 8. Twenty identical VMs on four identical nodes (symmetry pruning).
	cases = append(cases, stressCase{
		name: "identical-vms-identical-nodes",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 12, TotalRAM: 8192},
			{ID: "n2", Healthy: true, TotalCPU: 12, TotalRAM: 8192},
			{ID: "n3", Healthy: true, TotalCPU: 12, TotalRAM: 8192},
			{ID: "n4", Healthy: true, TotalCPU: 12, TotalRAM: 8192},
		},
		reqs: vms("vm", 20, 2, 1024),
		want: true,
	})

	// 9. Maximum-size nodes (64 cores, 524288 MiB) with large VMs.
	cases = append(cases, stressCase{
		name: "max-size-nodes",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 64, TotalRAM: 524288},
			{ID: "n2", Healthy: true, TotalCPU: 64, TotalRAM: 524288},
			{ID: "n3", Healthy: true, TotalCPU: 64, TotalRAM: 524288},
			{ID: "n4", Healthy: true, TotalCPU: 64, TotalRAM: 524288},
		},
		reqs: concat(
			vms("cpu-heavy", 10, 10, 100000),
			vms("ram-heavy", 10, 8, 80000),
		),
		want: true,
	})

	// 10. VMs at exactly 80% quota, one per node, tiny VMs fill the rest
	// exactly to capacity on both dimensions.
	cases = append(cases, stressCase{
		name: "quota-exact-fill-exact",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
			{ID: "n2", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
			{ID: "n3", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
			{ID: "n4", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
		},
		reqs: concat(
			vms("exact80", 4, 16, 1600), // exactly 80% of totals
			vms("tiny", 16, 1, 100),
		),
		want: true,
	})

	// 11. One VM 1 core over the quota sinks the whole batch.
	cases = append(cases, stressCase{
		name: "one-over-quota-sinks-batch",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
			{ID: "n2", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
			{ID: "n3", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
			{ID: "n4", Healthy: true, TotalCPU: 20, TotalRAM: 2000},
		},
		reqs: concat(
			vms("tiny", 19, 1, 100),
			[]Request{vm("over-quota", 17, 100)}, // quota is 16
		),
		want: false,
	})

	// 12. High existing usage (70%); small VMs fill most of the remainder.
	cases = append(cases, stressCase{
		name: "high-existing-usage",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 20, TotalRAM: 20480, UsedCPU: 14, UsedRAM: 14336},
			{ID: "n2", Healthy: true, TotalCPU: 20, TotalRAM: 20480, UsedCPU: 14, UsedRAM: 14336},
			{ID: "n3", Healthy: true, TotalCPU: 20, TotalRAM: 20480, UsedCPU: 14, UsedRAM: 14336},
			{ID: "n4", Healthy: true, TotalCPU: 20, TotalRAM: 20480, UsedCPU: 14, UsedRAM: 14336},
		},
		reqs: vms("vm", 20, 1, 1024),
		want: true,
	})

	// 13. Skewed nodes vs skewed VMs: CPU-rich nodes pair with CPU-heavy
	// VMs, RAM-rich nodes with RAM-heavy VMs.
	cases = append(cases, stressCase{
		name: "skewed-nodes-skewed-vms",
		nodes: []Node{
			{ID: "cpu1", Healthy: true, TotalCPU: 32, TotalRAM: 4096},
			{ID: "cpu2", Healthy: true, TotalCPU: 32, TotalRAM: 4096},
			{ID: "ram1", Healthy: true, TotalCPU: 8, TotalRAM: 65536},
			{ID: "ram2", Healthy: true, TotalCPU: 8, TotalRAM: 65536},
		},
		reqs: concat(
			vms("cpu-vm", 10, 2, 200),
			vms("ram-vm", 10, 1, 5000),
		),
		want: true,
	})

	// 14. Two-dimension tight feasible: both CPU and RAM land near capacity.
	cases = append(cases, stressCase{
		name: "two-dim-tight-feasible",
		nodes: []Node{
			{ID: "n1", Healthy: true, TotalCPU: 16, TotalRAM: 8192},
			{ID: "n2", Healthy: true, TotalCPU: 16, TotalRAM: 8192},
			{ID: "n3", Healthy: true, TotalCPU: 16, TotalRAM: 8192},
			{ID: "n4", Healthy: true, TotalCPU: 16, TotalRAM: 8192},
		},
		reqs: concat(
			vms("a", 16, 3, 1536),
			vms("b", 4, 2, 2048),
		),
		want: true,
	})

	return cases
}

func TestStressFixedSamples(t *testing.T) {
	cases := buildStressCases()
	if len(cases) < 12 {
		t.Fatalf("need at least 12 stress cases, got %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.nodes) != 4 {
				t.Fatalf("stress case must use 4 nodes, got %d", len(tc.nodes))
			}
			if len(tc.reqs) != 20 {
				t.Fatalf("stress case must use 20 VMs, got %d", len(tc.reqs))
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			res, err := Plan(tc.nodes, tc.reqs)
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatalf("Plan error: %v", err)
			}
			alloc := after.TotalAlloc - before.TotalAlloc
			t.Logf("feasible=%v objective=%d/%d (%.4f) elapsed=%s extra-alloc=%.1f MiB",
				res.Feasible, res.Objective.Num, res.Objective.Den,
				res.Objective.Float64(), elapsed, float64(alloc)/(1<<20))
			if res.Feasible != tc.want {
				t.Fatalf("feasibility = %v, want %v", res.Feasible, tc.want)
			}
			if elapsed > 10*time.Second {
				t.Fatalf("Plan took %s, limit is 10s", elapsed)
			}
			if alloc > 256<<20 {
				t.Fatalf("Plan allocated %.1f MiB, limit is 256 MiB", float64(alloc)/(1<<20))
			}
			if !res.Feasible {
				return
			}
			chk, err := Check(tc.nodes, tc.reqs, res.Placement)
			if err != nil {
				t.Fatalf("Check error: %v", err)
			}
			if !chk.Valid {
				t.Fatalf("Check rejected Plan output: %s", chk.Reason)
			}
			if chk.Objective != res.Objective {
				t.Fatalf("Check objective %v != Plan objective %v", chk.Objective, res.Objective)
			}
		})
	}
}
