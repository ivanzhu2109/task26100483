package vmplacer

import "sort"

// solve 用分支定界在固定快照上求精确最优放置。
// 目标：最小化 U = max_{健康节点 i} max(CPU利用率_i, RAM利用率_i)。
// 所有比较用 int64 交叉相乘完成，不经过浮点，保证最优性判断精确。
func solve(nodes []Node, reqs []VMRequest) (*Result, error) {
	var hn []Node
	for _, n := range nodes {
		if n.Healthy {
			hn = append(hn, n)
		}
	}
	if len(hn) == 0 {
		return nil, ErrInfeasible
	}
	// 快速排除：某台 VM 在所有健康节点上都不满足 80% 配额。
	for _, r := range reqs {
		ok := false
		for _, n := range hn {
			if fitsQuota(r, n) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, ErrInfeasible
		}
	}

	s := newSolver(hn, reqs)
	s.search()
	if !s.found {
		return nil, ErrInfeasible
	}
	placement := make(map[string]string, len(reqs))
	for vi, ni := range s.bestAssign {
		placement[s.vms[vi].ID] = s.nodes[ni].ID
	}
	return &Result{Placement: placement, Objective: s.best}, nil
}

type solver struct {
	nodes []Node      // 仅健康节点
	vms   []VMRequest // 按难度降序
	cpu   []int64     // 各节点当前 CPU 占用（含原有）
	ram   []int64
	quota [][]bool // quota[vi][ni]：VM 是否满足节点 80% 配额

	assign     []int
	bestAssign []int
	best       Ratio
	found      bool

	remCPU int64 // 未分配 VM 的 CPU 需求之和
	remRAM int64
	totCPU int64 // 健康节点容量之和
	totRAM int64
}

func newSolver(nodes []Node, reqs []VMRequest) *solver {
	s := &solver{nodes: nodes, remCPU: 0, remRAM: 0}
	s.cpu = make([]int64, len(nodes))
	s.ram = make([]int64, len(nodes))
	for i, n := range nodes {
		s.cpu[i] = n.CPUUsed
		s.ram[i] = n.RAMUsed
		s.totCPU += n.CPUTotal
		s.totRAM += n.RAMTotal
	}
	// 难度降序：相对最大容量的需求比例越大越先放，尽快暴露冲突、尽早拿到好 incumbent。
	var maxCPU, maxRAM int64 = 1, 1
	for _, n := range nodes {
		if n.CPUTotal > maxCPU {
			maxCPU = n.CPUTotal
		}
		if n.RAMTotal > maxRAM {
			maxRAM = n.RAMTotal
		}
	}
	s.vms = make([]VMRequest, len(reqs))
	copy(s.vms, reqs)
	sort.SliceStable(s.vms, func(a, b int) bool {
		an, ad := maxRatio(s.vms[a].CPU, maxCPU, s.vms[a].RAM, maxRAM)
		bn, bd := maxRatio(s.vms[b].CPU, maxCPU, s.vms[b].RAM, maxRAM)
		if v := an*bd - bn*ad; v != 0 {
			return v > 0
		}
		return s.vms[a].ID < s.vms[b].ID
	})
	s.quota = make([][]bool, len(s.vms))
	for i, r := range s.vms {
		s.quota[i] = make([]bool, len(nodes))
		for j, n := range nodes {
			s.quota[i][j] = fitsQuota(r, n)
		}
		s.remCPU += r.CPU
		s.remRAM += r.RAM
	}
	s.assign = make([]int, len(s.vms))
	s.bestAssign = make([]int, len(s.vms))
	return s
}

// maxRatio 返回 max(a/b, c/d) 的分子分母（未约分，仅用于排序比较）。
func maxRatio(a, b, c, d int64) (int64, int64) {
	if a*d >= c*b {
		return a, b
	}
	return c, d
}

func (s *solver) search() { s.dfs(0) }

func (s *solver) dfs(idx int) {
	if idx == len(s.vms) {
		obj := s.curMax()
		if !s.found || obj.Cmp(s.best) < 0 {
			s.found = true
			s.best = obj
			copy(s.bestAssign, s.assign)
		}
		return
	}
	vm := s.vms[idx]
	s.remCPU -= vm.CPU
	s.remRAM -= vm.RAM
	defer func() { s.remCPU += vm.CPU; s.remRAM += vm.RAM }()

	if s.found {
		// 下界剪枝：部分解当前最大利用率与两种资源的聚合下界都不会再变小。
		lb := s.lowerBound()
		if lb.Cmp(s.best) >= 0 {
			return
		}
	}
	// 前向可行性：每台剩余 VM 至少能放进一个节点（配额 + 当前剩余容量）。
	for j := idx; j < len(s.vms); j++ {
		r := s.vms[j]
		ok := false
		for i := range s.nodes {
			if s.quota[j][i] && s.cpu[i]+r.CPU <= s.nodes[i].CPUTotal && s.ram[i]+r.RAM <= s.nodes[i].RAMTotal {
				ok = true
				break
			}
		}
		if !ok {
			return
		}
	}

	// 分支顺序：按放置后节点最大利用率升序，优先往更空的节点放，尽早压低 incumbent。
	order := s.nodeOrder(idx, vm)
	for _, i := range order {
		if s.skipSymmetric(i) {
			continue
		}
		s.cpu[i] += vm.CPU
		s.ram[i] += vm.RAM
		s.assign[idx] = i
		s.dfs(idx + 1)
		s.cpu[i] -= vm.CPU
		s.ram[i] -= vm.RAM
	}
}

// nodeOrder 返回可接纳 vm 的节点下标，按放置后该节点最大利用率升序。
func (s *solver) nodeOrder(idx int, vm VMRequest) []int {
	var order []int
	for i, n := range s.nodes {
		if !s.quota[idx][i] {
			continue
		}
		if s.cpu[i]+vm.CPU > n.CPUTotal || s.ram[i]+vm.RAM > n.RAMTotal {
			continue
		}
		order = append(order, i)
	}
	sort.Slice(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		na, nb := s.nodes[ia], s.nodes[ib]
		// 比较 max((cpu+vm)/totCPU, (ram+vm)/totRAM)
		ca, ra := s.cpu[ia]+vm.CPU, s.ram[ia]+vm.RAM
		cb, rb := s.cpu[ib]+vm.CPU, s.ram[ib]+vm.RAM
		// 先比较 CPU 利用率与 RAM 利用率的各自最大值
		aNum, aDen := maxRatio(ca, na.CPUTotal, ra, na.RAMTotal)
		bNum, bDen := maxRatio(cb, nb.CPUTotal, rb, nb.RAMTotal)
		if v := aNum*bDen - bNum*aDen; v != 0 {
			return v < 0
		}
		return ia < ib
	})
	return order
}

// skipSymmetric：与更早节点状态完全一致（容量与当前占用都相同）的节点只展开第一个。
func (s *solver) skipSymmetric(i int) bool {
	for j := 0; j < i; j++ {
		a, b := s.nodes[j], s.nodes[i]
		if a.CPUTotal == b.CPUTotal && a.RAMTotal == b.RAMTotal &&
			s.cpu[j] == s.cpu[i] && s.ram[j] == s.ram[i] {
			return true
		}
	}
	return false
}

// curMax 当前各健康节点两种资源利用率的最大值（精确比例）。
func (s *solver) curMax() Ratio {
	var best Ratio
	set := false
	for i, n := range s.nodes {
		for _, u := range [2]Ratio{NewRatio(s.cpu[i], n.CPUTotal), NewRatio(s.ram[i], n.RAMTotal)} {
			if !set || u.Cmp(best) > 0 {
				best = u
				set = true
			}
		}
	}
	return best
}

// lowerBound 当前部分解能达到的目标值下界：
// max(当前最大利用率, 总CPU负载/总CPU容量, 总RAM负载/总RAM容量)。
// 最大利用率不小于按容量加权的平均负载。
func (s *solver) lowerBound() Ratio {
	lb := s.curMax()
	var loadCPU, loadRAM int64
	for i := range s.nodes {
		loadCPU += s.cpu[i]
		loadRAM += s.ram[i]
	}
	for _, u := range [2]Ratio{
		NewRatio(loadCPU+s.remCPU, s.totCPU),
		NewRatio(loadRAM+s.remRAM, s.totRAM),
	} {
		if u.Cmp(lb) > 0 {
			lb = u
		}
	}
	return lb
}
