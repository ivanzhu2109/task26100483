package vmplanner

import "sort"

// solver holds the immutable problem data and the mutable search state for
// one exact branch-and-bound run over a fixed snapshot.
type solver struct {
	nodes []Node
	reqs  []Request // sorted: most constrained first

	// eligible[i] lists healthy node indices that satisfy the per-VM
	// quota for reqs[i] (capacity is checked dynamically).
	eligible [][]int

	usedCPU []int64
	usedRAM []int64

	// Aggregate capacity/demand over healthy nodes, for the lower bound.
	healthyCPU int64
	healthyRAM int64

	assign  []int // current assignment: request index -> node index
	best    []int
	hasBest bool
	bestObj Ratio

	// sufMaxCPU[i]/sufMaxRAM[i] is the largest single demand among
	// reqs[i:], for the "largest remaining VM" lower bound.
	sufMaxCPU []int64
	sufMaxRAM []int64

	// visited is a transposition table over (depth, usage vector); the
	// remaining request set is determined by depth, so a revisited state
	// can only repeat work. Insertions are capped for memory safety;
	// the cap never affects completeness, only pruning.
	visited map[[9]int64]struct{}
}

// solve runs an exact branch-and-bound search and returns the optimal
// whole-batch placement, or Feasible == false iff none exists. It never
// treats search effort as infeasibility: the search space is finite and
// is explored exhaustively subject only to sound pruning.
func solve(nodes []Node, requests []Request) (PlanResult, error) {
	s := &solver{
		nodes:   nodes,
		usedCPU: make([]int64, len(nodes)),
		usedRAM: make([]int64, len(nodes)),
		assign:  make([]int, len(requests)),
		best:    make([]int, len(requests)),
	}
	for i, n := range nodes {
		s.usedCPU[i] = n.UsedCPU
		s.usedRAM[i] = n.UsedRAM
		if n.Healthy {
			s.healthyCPU += n.TotalCPU
			s.healthyRAM += n.TotalRAM
		}
	}

	// Eligibility: healthy and within the per-VM 80%-of-total quota.
	s.eligible = make([][]int, len(requests))
	for i, r := range requests {
		for j, n := range nodes {
			if n.Healthy && fitsQuota(r.CPU, r.RAM, n) {
				s.eligible[i] = append(s.eligible[i], j)
			}
		}
		if len(s.eligible[i]) == 0 {
			return PlanResult{Feasible: false}, nil
		}
	}

	// Order requests: fewest eligible nodes first, then larger demand
	// first, so constrained VMs are placed before flexible ones.
	order := make([]int, len(requests))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		ra, rb := requests[order[a]], requests[order[b]]
		ea, eb := len(s.eligible[order[a]]), len(s.eligible[order[b]])
		if ea != eb {
			return ea < eb
		}
		da := ra.CPU*s.healthyRAM + ra.RAM*s.healthyCPU
		db := rb.CPU*s.healthyRAM + rb.RAM*s.healthyCPU
		if da != db {
			return da > db
		}
		return ra.ID < rb.ID
	})
	s.reqs = make([]Request, len(requests))
	elig := make([][]int, len(requests))
	for i, oi := range order {
		s.reqs[i] = requests[oi]
		elig[i] = s.eligible[oi]
	}
	s.eligible = elig

	s.sufMaxCPU = make([]int64, len(s.reqs)+1)
	s.sufMaxRAM = make([]int64, len(s.reqs)+1)
	for i := len(s.reqs) - 1; i >= 0; i-- {
		s.sufMaxCPU[i] = max64(s.sufMaxCPU[i+1], s.reqs[i].CPU)
		s.sufMaxRAM[i] = max64(s.sufMaxRAM[i+1], s.reqs[i].RAM)
	}
	s.visited = make(map[[9]int64]struct{})

	s.greedyIncumbent()
	var remCPU, remRAM int64
	for _, r := range s.reqs {
		remCPU += r.CPU
		remRAM += r.RAM
	}
	s.search(0, remCPU, remRAM)

	if !s.hasBest {
		return PlanResult{Feasible: false}, nil
	}
	placement := make(Placement, len(requests))
	for i, r := range s.reqs {
		placement[r.ID] = s.nodes[s.best[i]].ID
	}
	return PlanResult{Feasible: true, Placement: placement, Objective: s.bestObj}, nil
}

// currentMaxUtil is the max utilization over healthy nodes of the current
// partial state. Utilization only grows as more VMs are assigned, so this
// is a valid lower bound on the final objective.
func (s *solver) currentMaxUtil() Ratio {
	best := Ratio{Num: 0, Den: 1}
	for j, n := range s.nodes {
		if !n.Healthy {
			continue
		}
		if c := makeRatio(s.usedCPU[j], n.TotalCPU); best.less(c) {
			best = c
		}
		if r := makeRatio(s.usedRAM[j], n.TotalRAM); best.less(r) {
			best = r
		}
	}
	return best
}

// lowerBound combines three sound bounds on the final objective:
//  1. monotone: utilization never decreases as more VMs are assigned;
//  2. aggregate: all remaining demand lands on healthy nodes, so the final
//     max utilization is at least (used + remaining) / capacity summed
//     over healthy nodes, per resource;
//  3. largest-VM: the largest remaining VM lands on some node, so the
//     final max is at least the minimum over nodes of (used + max)/cap.
func (s *solver) lowerBound(depth int, remCPU, remRAM int64) Ratio {
	lb := s.currentMaxUtil()
	if c := makeRatio(s.sumUsedCPU()+remCPU, s.healthyCPU); lb.less(c) {
		lb = c
	}
	if r := makeRatio(s.sumUsedRAM()+remRAM, s.healthyRAM); lb.less(r) {
		lb = r
	}
	if depth < len(s.reqs) {
		mc, mr := s.sufMaxCPU[depth], s.sufMaxRAM[depth]
		var bc, br Ratio
		first := true
		for j, n := range s.nodes {
			if !n.Healthy {
				continue
			}
			c := makeRatio(s.usedCPU[j]+mc, n.TotalCPU)
			r := makeRatio(s.usedRAM[j]+mr, n.TotalRAM)
			if first || c.less(bc) {
				bc = c
			}
			if first || r.less(br) {
				br = r
			}
			first = false
		}
		if lb.less(bc) {
			lb = bc
		}
		if lb.less(br) {
			lb = br
		}
	}
	return lb
}

func (s *solver) sumUsedCPU() int64 {
	var t int64
	for j, n := range s.nodes {
		if n.Healthy {
			t += s.usedCPU[j]
		}
	}
	return t
}

func (s *solver) sumUsedRAM() int64 {
	var t int64
	for j, n := range s.nodes {
		if n.Healthy {
			t += s.usedRAM[j]
		}
	}
	return t
}

// greedyIncumbent tries to build a good (not necessarily optimal) complete
// assignment to seed the upper bound. Its failure proves nothing; the
// exact search below decides feasibility.
func (s *solver) greedyIncumbent() {
	savedCPU := append([]int64(nil), s.usedCPU...)
	savedRAM := append([]int64(nil), s.usedRAM...)
	trial := make([]int, len(s.reqs))
	ok := true
	for i, r := range s.reqs {
		bestJ, bestU := -1, Ratio{}
		for _, j := range s.eligible[i] {
			n := s.nodes[j]
			if s.usedCPU[j]+r.CPU > n.TotalCPU || s.usedRAM[j]+r.RAM > n.TotalRAM {
				continue
			}
			s.usedCPU[j] += r.CPU
			s.usedRAM[j] += r.RAM
			u := s.currentMaxUtil()
			s.usedCPU[j] -= r.CPU
			s.usedRAM[j] -= r.RAM
			if bestJ == -1 || u.less(bestU) {
				bestJ, bestU = j, u
			}
		}
		if bestJ == -1 {
			ok = false
			break
		}
		s.usedCPU[bestJ] += r.CPU
		s.usedRAM[bestJ] += r.RAM
		trial[i] = bestJ
	}
	if ok {
		s.hasBest = true
		s.bestObj = s.currentMaxUtil()
		copy(s.best, trial)
	}
	copy(s.usedCPU, savedCPU)
	copy(s.usedRAM, savedRAM)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// stateKey identifies a node by its totals and current usage; two nodes
// with equal keys are interchangeable for the remaining search.
type stateKey struct {
	totalCPU, totalRAM, usedCPU, usedRAM int64
}

// search assigns reqs[depth] to a node. remCPU/remRAM are the total demand
// of reqs[depth:] (including reqs[depth]).
func (s *solver) search(depth int, remCPU, remRAM int64) {
	if s.hasBest && s.lowerBound(depth, remCPU, remRAM).geq(s.bestObj) {
		return
	}
	var key [9]int64
	key[0] = int64(depth)
	for j := range s.nodes {
		key[1+2*j] = s.usedCPU[j]
		key[2+2*j] = s.usedRAM[j]
	}
	if _, dup := s.visited[key]; dup {
		return
	}
	if len(s.visited) < 1<<20 {
		s.visited[key] = struct{}{}
	}
	if depth == len(s.reqs) {
		obj := s.currentMaxUtil()
		if !s.hasBest || obj.less(s.bestObj) {
			s.hasBest = true
			s.bestObj = obj
			copy(s.best, s.assign)
		}
		return
	}
	r := s.reqs[depth]
	seen := make(map[stateKey]bool, len(s.eligible[depth]))
	for _, j := range s.eligible[depth] {
		n := s.nodes[j]
		if s.usedCPU[j]+r.CPU > n.TotalCPU || s.usedRAM[j]+r.RAM > n.TotalRAM {
			continue
		}
		k := stateKey{n.TotalCPU, n.TotalRAM, s.usedCPU[j], s.usedRAM[j]}
		if seen[k] {
			continue
		}
		seen[k] = true
		s.usedCPU[j] += r.CPU
		s.usedRAM[j] += r.RAM
		s.assign[depth] = j
		s.search(depth+1, remCPU-r.CPU, remRAM-r.RAM)
		s.usedCPU[j] -= r.CPU
		s.usedRAM[j] -= r.RAM
	}
}
