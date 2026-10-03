package vmplacer

import "fmt"

// Check 校验调用者给出的整批放置：每台 VM 恰好分到一个健康节点、
// 单 VM 不超过目标节点总容量的 80%、节点原有占用加新分配不超总容量，
// 并计算与 Plan 同一定义的精确目标值 U（所有健康节点放置后
// CPU 与 RAM 利用率的最大值）。它不判断方案是否最优。
func Check(nodes []Node, reqs []VMRequest, placement map[string]string) (Ratio, error) {
	if err := validate(nodes, reqs); err != nil {
		return Ratio{}, err
	}
	byID := make(map[string]int, len(nodes))
	for i, n := range nodes {
		byID[n.ID] = i
	}
	// 完整性：每台 VM 恰好出现一次，且不允许出现未知 VM。
	seen := make(map[string]bool, len(reqs))
	for vmID, nodeID := range placement {
		if !containsVM(reqs, vmID) {
			return Ratio{}, fmt.Errorf("vmplacer: placement assigns unknown VM %q", vmID)
		}
		if seen[vmID] {
			return Ratio{}, fmt.Errorf("vmplacer: VM %q assigned more than once", vmID)
		}
		seen[vmID] = true
		if _, ok := byID[nodeID]; !ok {
			return Ratio{}, fmt.Errorf("vmplacer: VM %q assigned to unknown node %q", vmID, nodeID)
		}
	}
	for _, r := range reqs {
		if !seen[r.ID] {
			return Ratio{}, fmt.Errorf("vmplacer: VM %q is not assigned", r.ID)
		}
	}

	cpuUsed := make([]int64, len(nodes))
	ramUsed := make([]int64, len(nodes))
	for i, n := range nodes {
		cpuUsed[i] = n.CPUUsed
		ramUsed[i] = n.RAMUsed
	}
	for _, r := range reqs {
		ni := byID[placement[r.ID]]
		n := nodes[ni]
		if !n.Healthy {
			return Ratio{}, fmt.Errorf("vmplacer: VM %q assigned to unhealthy node %q", r.ID, n.ID)
		}
		if !fitsQuota(r, n) {
			return Ratio{}, fmt.Errorf("vmplacer: VM %q exceeds 80%% quota of node %q", r.ID, n.ID)
		}
		cpuUsed[ni] += r.CPU
		ramUsed[ni] += r.RAM
	}
	for i, n := range nodes {
		if cpuUsed[i] > n.CPUTotal {
			return Ratio{}, fmt.Errorf("vmplacer: node %q CPU overcommitted: %d/%d", n.ID, cpuUsed[i], n.CPUTotal)
		}
		if ramUsed[i] > n.RAMTotal {
			return Ratio{}, fmt.Errorf("vmplacer: node %q RAM overcommitted: %d/%d", n.ID, ramUsed[i], n.RAMTotal)
		}
	}
	return objective(nodes, cpuUsed, ramUsed)
}

func containsVM(reqs []VMRequest, id string) bool {
	for _, r := range reqs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// objective 计算 U：所有健康节点上 max(CPU利用率, RAM利用率) 的最大值。
// 分母取各节点对应总容量，结果为精确比例。
func objective(nodes []Node, cpuUsed, ramUsed []int64) (Ratio, error) {
	best := Ratio{Num: -1, Den: 1}
	for i, n := range nodes {
		if !n.Healthy {
			continue
		}
		for _, u := range [2]Ratio{NewRatio(cpuUsed[i], n.CPUTotal), NewRatio(ramUsed[i], n.RAMTotal)} {
			if best.Num < 0 || u.Cmp(best) > 0 {
				best = u
			}
		}
	}
	if best.Num < 0 {
		return Ratio{}, fmt.Errorf("vmplacer: no healthy node")
	}
	return best, nil
}
