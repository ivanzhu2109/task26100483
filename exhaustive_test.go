package vmplanner

import (
	"math/rand"
	"testing"
)

// referenceSolve is an independent exhaustive reference implementation.
// It enumerates every assignment of requests to healthy nodes, filters by
// the per-VM quota and capacity rules, and keeps the exact best objective
// as an unnormalized (num, den) pair compared by cross-multiplication.
// It shares no search logic with the solver under test.
func referenceSolve(nodes []Node, reqs []Request) (feasible bool, bestNum, bestDen int64) {
	var healthy []int
	for i, n := range nodes {
		if n.Healthy {
			healthy = append(healthy, i)
		}
	}
	// Per-VM quota: demand*5 <= total*4, checked on TOTAL capacity.
	eligible := make([][]int, len(reqs))
	for i, r := range reqs {
		for _, j := range healthy {
			n := nodes[j]
			if r.CPU*5 <= 4*n.TotalCPU && r.RAM*5 <= 4*n.TotalRAM {
				eligible[i] = append(eligible[i], j)
			}
		}
		if len(eligible[i]) == 0 {
			return false, 0, 1
		}
	}
	usedCPU := make([]int64, len(nodes))
	usedRAM := make([]int64, len(nodes))
	for i, n := range nodes {
		usedCPU[i] = n.UsedCPU
		usedRAM[i] = n.UsedRAM
	}
	assign := make([]int, len(reqs))
	var rec func(d int)
	rec = func(d int) {
		if d == len(reqs) {
			// Objective: max over healthy nodes of used/total per resource.
			bn, bd := int64(0), int64(1)
			for _, j := range healthy {
				n := nodes[j]
				for _, frac := range [2][2]int64{
					{usedCPU[j], n.TotalCPU},
					{usedRAM[j], n.TotalRAM},
				} {
					if frac[0]*bd > bn*frac[1] {
						bn, bd = frac[0], frac[1]
					}
				}
			}
			if !feasible || bn*bestDen < bestNum*bd {
				feasible, bestNum, bestDen = true, bn, bd
			}
			return
		}
		r := reqs[d]
		for _, j := range eligible[d] {
			n := nodes[j]
			if usedCPU[j]+r.CPU > n.TotalCPU || usedRAM[j]+r.RAM > n.TotalRAM {
				continue
			}
			usedCPU[j] += r.CPU
			usedRAM[j] += r.RAM
			assign[d] = j
			rec(d + 1)
			usedCPU[j] -= r.CPU
			usedRAM[j] -= r.RAM
		}
	}
	rec(0)
	return feasible, bestNum, bestDen
}

// aggregateSufficientButInfeasible reports whether the instance has enough
// total remaining CPU and RAM on healthy nodes, and every VM fits some
// node individually, yet the batch cannot be placed.
func aggregateSufficientButInfeasible(nodes []Node, reqs []Request, feasible bool) bool {
	if feasible {
		return false
	}
	var remCPU, remRAM, demCPU, demRAM int64
	for _, n := range nodes {
		if n.Healthy {
			remCPU += n.TotalCPU - n.UsedCPU
			remRAM += n.TotalRAM - n.UsedRAM
		}
	}
	for _, r := range reqs {
		demCPU += r.CPU
		demRAM += r.RAM
		ok := false
		for _, n := range nodes {
			if n.Healthy && r.CPU*5 <= 4*n.TotalCPU && r.RAM*5 <= 4*n.TotalRAM &&
				r.CPU <= n.TotalCPU-n.UsedCPU && r.RAM <= n.TotalRAM-n.UsedRAM {
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

func TestExhaustiveCrossCheck(t *testing.T) {
	rng := rand.New(rand.NewSource(20261004))
	tricky := 0
	total := 0

	run := func(nodes []Node, reqs []Request) {
		total++
		res, err := Plan(nodes, reqs)
		if err != nil {
			t.Fatalf("instance %d: Plan error: %v", total, err)
		}
		refOK, refNum, refDen := referenceSolve(nodes, reqs)
		if res.Feasible != refOK {
			t.Fatalf("instance %d: feasibility mismatch: Plan=%v reference=%v\nnodes=%+v\nreqs=%+v",
				total, res.Feasible, refOK, nodes, reqs)
		}
		if !res.Feasible {
			if aggregateSufficientButInfeasible(nodes, reqs, false) {
				tricky++
			}
			return
		}
		// Exact objective equality via cross-multiplication.
		if res.Objective.Num*refDen != refNum*res.Objective.Den {
			t.Fatalf("instance %d: objective mismatch: Plan=%d/%d reference=%d/%d\nnodes=%+v\nreqs=%+v",
				total, res.Objective.Num, res.Objective.Den, refNum, refDen, nodes, reqs)
		}
		chk, err := Check(nodes, reqs, res.Placement)
		if err != nil || !chk.Valid {
			t.Fatalf("instance %d: Check rejected Plan output: %v %v", total, err, chk.Reason)
		}
		if chk.Objective != res.Objective {
			t.Fatalf("instance %d: Check objective %v != Plan objective %v", total, chk.Objective, res.Objective)
		}
	}

	// Fixed tricky instances: two-dimensional aggregate remaining capacity
	// suffices and every VM fits individually, yet the batch is infeasible.
	fixed := []struct {
		nodes []Node
		reqs  []Request
	}{
		{
			nodes: []Node{
				{ID: "a", Healthy: true, TotalCPU: 10, TotalRAM: 10},
				{ID: "b", Healthy: true, TotalCPU: 10, TotalRAM: 10},
			},
			reqs: []Request{
				{ID: "v1", CPU: 6, RAM: 6},
				{ID: "v2", CPU: 6, RAM: 6},
				{ID: "v3", CPU: 6, RAM: 6},
			},
		},
		{
			// CPU-heavy and RAM-heavy VMs block each other per node.
			nodes: []Node{
				{ID: "a", Healthy: true, TotalCPU: 10, TotalRAM: 1000},
				{ID: "b", Healthy: true, TotalCPU: 10, TotalRAM: 1000},
				{ID: "c", Healthy: true, TotalCPU: 10, TotalRAM: 1000},
			},
			reqs: []Request{
				{ID: "c1", CPU: 8, RAM: 100},
				{ID: "c2", CPU: 8, RAM: 100},
				{ID: "c3", CPU: 8, RAM: 100},
				{ID: "m1", CPU: 1, RAM: 800},
				{ID: "m2", CPU: 1, RAM: 800},
				{ID: "m3", CPU: 1, RAM: 800},
			},
		},
	}
	for _, f := range fixed {
		run(f.nodes, f.reqs)
	}

	// Random small instances: 2-4 heterogeneous nodes, 1-7 VMs.
	for total < 152 {
		nNodes := 2 + rng.Intn(3)
		var nodes []Node
		for i := 0; i < nNodes; i++ {
			totCPU := int64(2 + rng.Intn(15))         // 2..16 cores
			totRAM := int64(512 * (1 + rng.Intn(32))) // 512..16384 MiB
			n := Node{
				ID:       string(rune('a' + i)),
				Healthy:  rng.Intn(5) > 0, // 80% healthy
				TotalCPU: totCPU,
				TotalRAM: totRAM,
				UsedCPU:  rng.Int63n(totCPU + 1),
				UsedRAM:  rng.Int63n(totRAM + 1),
			}
			nodes = append(nodes, n)
		}
		nodes[0].Healthy = true // guarantee at least one healthy node
		nReqs := 1 + rng.Intn(7)
		var reqs []Request
		for i := 0; i < nReqs; i++ {
			reqs = append(reqs, Request{
				ID:  string(rune('A'+i)) + string(rune('0'+i)),
				CPU: 1 + rng.Int63n(8),
				RAM: 1 + rng.Int63n(4096),
			})
		}
		run(nodes, reqs)
	}
	if tricky == 0 {
		t.Fatal("no aggregate-sufficient-but-infeasible instance exercised")
	}
	t.Logf("cross-checked %d instances, %d aggregate-sufficient-but-infeasible", total, tricky)
}
