package vmplanner

import "fmt"

const (
	maxNodes    = 4
	minNodes    = 2
	maxTotalCPU = 64
	maxTotalRAM = 524288
	maxRequests = 20
	quotaPctNum = 4 // per-VM quota: demand <= 80% of node TOTAL capacity
	quotaPctDen = 5
)

// validateSnapshot checks structural input invariants shared by Plan and
// Check. It does not check placement feasibility.
func validateSnapshot(nodes []Node, requests []Request) error {
	if len(nodes) < minNodes || len(nodes) > maxNodes {
		return fmt.Errorf("vmplanner: need %d..%d nodes, got %d", minNodes, maxNodes, len(nodes))
	}
	seenNodes := make(map[string]bool, len(nodes))
	healthy := 0
	for _, n := range nodes {
		if n.ID == "" {
			return fmt.Errorf("vmplanner: node with empty ID")
		}
		if seenNodes[n.ID] {
			return fmt.Errorf("vmplanner: duplicate node ID %q", n.ID)
		}
		seenNodes[n.ID] = true
		if n.TotalCPU <= 0 || n.TotalCPU > maxTotalCPU {
			return fmt.Errorf("vmplanner: node %q CPU total %d out of range (1..%d)", n.ID, n.TotalCPU, maxTotalCPU)
		}
		if n.TotalRAM <= 0 || n.TotalRAM > maxTotalRAM {
			return fmt.Errorf("vmplanner: node %q RAM total %d out of range (1..%d)", n.ID, n.TotalRAM, maxTotalRAM)
		}
		if n.UsedCPU < 0 || n.UsedCPU > n.TotalCPU || n.UsedRAM < 0 || n.UsedRAM > n.TotalRAM {
			return fmt.Errorf("vmplanner: node %q existing usage exceeds capacity", n.ID)
		}
		if n.Healthy {
			healthy++
		}
	}
	if healthy == 0 {
		return fmt.Errorf("vmplanner: no healthy node")
	}
	if len(requests) < 1 || len(requests) > maxRequests {
		return fmt.Errorf("vmplanner: need 1..%d requests, got %d", maxRequests, len(requests))
	}
	seenReqs := make(map[string]bool, len(requests))
	for _, r := range requests {
		if r.ID == "" {
			return fmt.Errorf("vmplanner: request with empty ID")
		}
		if seenReqs[r.ID] {
			return fmt.Errorf("vmplanner: duplicate request ID %q", r.ID)
		}
		seenReqs[r.ID] = true
		if r.CPU <= 0 || r.RAM <= 0 {
			return fmt.Errorf("vmplanner: request %q has non-positive demand", r.ID)
		}
	}
	return nil
}

// fitsQuota reports whether a single VM with the given demand satisfies the
// per-VM quota on a node: demand <= 80% of the node's TOTAL capacity,
// compared exactly as demand*5 <= total*4.
func fitsQuota(cpu, ram int64, n Node) bool {
	return cpu*quotaPctDen <= quotaPctNum*n.TotalCPU &&
		ram*quotaPctDen <= quotaPctNum*n.TotalRAM
}

// objective computes U: the maximum, over all healthy nodes, of the
// post-placement CPU and RAM utilizations (used/total), exactly.
func objective(nodes []Node, usedCPU, usedRAM []int64) Ratio {
	best := Ratio{Num: 0, Den: 1}
	for i, n := range nodes {
		if !n.Healthy {
			continue
		}
		if c := makeRatio(usedCPU[i], n.TotalCPU); best.less(c) {
			best = c
		}
		if r := makeRatio(usedRAM[i], n.TotalRAM); best.less(r) {
			best = r
		}
	}
	return best
}

// Check validates a caller-constructed placement for the whole batch and
// computes its objective with the same definition as Plan. It does not
// judge optimality.
func Check(nodes []Node, requests []Request, placement Placement) (CheckResult, error) {
	if err := validateSnapshot(nodes, requests); err != nil {
		return CheckResult{}, err
	}
	if len(placement) != len(requests) {
		return CheckResult{Valid: false, Reason: fmt.Sprintf("placement covers %d of %d requests", len(placement), len(requests))}, nil
	}
	nodeIdx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		nodeIdx[n.ID] = i
	}
	usedCPU := make([]int64, len(nodes))
	usedRAM := make([]int64, len(nodes))
	for i, n := range nodes {
		usedCPU[i] = n.UsedCPU
		usedRAM[i] = n.UsedRAM
	}
	for _, r := range requests {
		target, ok := placement[r.ID]
		if !ok {
			return CheckResult{Valid: false, Reason: fmt.Sprintf("request %q is not placed", r.ID)}, nil
		}
		ni, ok := nodeIdx[target]
		if !ok {
			return CheckResult{Valid: false, Reason: fmt.Sprintf("request %q placed on unknown node %q", r.ID, target)}, nil
		}
		n := nodes[ni]
		if !n.Healthy {
			return CheckResult{Valid: false, Reason: fmt.Sprintf("request %q placed on unhealthy node %q", r.ID, target)}, nil
		}
		if !fitsQuota(r.CPU, r.RAM, n) {
			return CheckResult{Valid: false, Reason: fmt.Sprintf("request %q exceeds the 80%% per-VM quota of node %q", r.ID, target)}, nil
		}
		usedCPU[ni] += r.CPU
		usedRAM[ni] += r.RAM
	}
	for i, n := range nodes {
		if usedCPU[i] > n.TotalCPU {
			return CheckResult{Valid: false, Reason: fmt.Sprintf("node %q CPU overcommitted: %d/%d", n.ID, usedCPU[i], n.TotalCPU)}, nil
		}
		if usedRAM[i] > n.TotalRAM {
			return CheckResult{Valid: false, Reason: fmt.Sprintf("node %q RAM overcommitted: %d/%d", n.ID, usedRAM[i], n.TotalRAM)}, nil
		}
	}
	return CheckResult{Valid: true, Objective: objective(nodes, usedCPU, usedRAM)}, nil
}
