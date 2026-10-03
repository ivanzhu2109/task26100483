package vmplacer

import "fmt"

// 输入快照的硬约束。
const (
	MinNodes = 2
	MaxNodes = 4
	MaxCPU   = 64
	MaxRAM   = 524288 // MiB
	MaxVMs   = 20
)

// Node 是集群中一台物理节点的固定快照。
type Node struct {
	ID       string
	Healthy  bool
	CPUTotal int64 // 总 CPU 核数，1..64
	RAMTotal int64 // 总 RAM，MiB，1..524288
	CPUUsed  int64 // 现有 VM 已占用 CPU
	RAMUsed  int64 // 现有 VM 已占用 RAM
}

// VMRequest 是一台待放置 VM 的资源需求。
type VMRequest struct {
	ID  string
	CPU int64 // 正整数核
	RAM int64 // 正整数 MiB
}

// Ratio 是精确比例 Num/Den（已约分，Den>0），用于目标值与利用率比较。
// 比较一律通过交叉相乘在 int64 内完成，不经过浮点。
type Ratio struct {
	Num int64
	Den int64
}

// NewRatio 构造并约分一个比例；den 必须为正。
func NewRatio(num, den int64) Ratio {
	if den <= 0 {
		panic("vmplacer: non-positive denominator")
	}
	if num < 0 {
		panic("vmplacer: negative numerator")
	}
	g := gcd(num, den)
	return Ratio{Num: num / g, Den: den / g}
}

// Cmp 精确比较两个比例：a<b 返回 -1，a==b 返回 0，a>b 返回 1。
// 交叉相乘的最大量级约为 (20*0.8*524288) * 524288 ≈ 4.4e12，远低于 int64 上限。
func (a Ratio) Cmp(b Ratio) int {
	l := a.Num * b.Den
	r := b.Num * a.Den
	switch {
	case l < r:
		return -1
	case l > r:
		return 1
	default:
		return 0
	}
}

func (a Ratio) String() string { return fmt.Sprintf("%d/%d", a.Num, a.Den) }

// Float 仅用于展示，不参与任何最优性判断。
func (a Ratio) Float() float64 { return float64(a.Num) / float64(a.Den) }

func gcd(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

// validate 校验快照与请求是否满足题目给定的输入约束。
func validate(nodes []Node, reqs []VMRequest) error {
	if len(nodes) < MinNodes || len(nodes) > MaxNodes {
		return fmt.Errorf("vmplacer: need %d..%d nodes, got %d", MinNodes, MaxNodes, len(nodes))
	}
	if len(reqs) < 1 || len(reqs) > MaxVMs {
		return fmt.Errorf("vmplacer: need 1..%d VMs, got %d", MaxVMs, len(reqs))
	}
	nodeIDs := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if n.ID == "" {
			return fmt.Errorf("vmplacer: node with empty ID")
		}
		if nodeIDs[n.ID] {
			return fmt.Errorf("vmplacer: duplicate node ID %q", n.ID)
		}
		nodeIDs[n.ID] = true
		if n.CPUTotal < 1 || n.CPUTotal > MaxCPU {
			return fmt.Errorf("vmplacer: node %q CPU total %d out of range 1..%d", n.ID, n.CPUTotal, MaxCPU)
		}
		if n.RAMTotal < 1 || n.RAMTotal > MaxRAM {
			return fmt.Errorf("vmplacer: node %q RAM total %d out of range 1..%d", n.ID, n.RAMTotal, MaxRAM)
		}
		if n.CPUUsed < 0 || n.RAMUsed < 0 || n.CPUUsed > n.CPUTotal || n.RAMUsed > n.RAMTotal {
			return fmt.Errorf("vmplacer: node %q existing usage exceeds capacity", n.ID)
		}
	}
	vmIDs := make(map[string]bool, len(reqs))
	for _, r := range reqs {
		if r.ID == "" {
			return fmt.Errorf("vmplacer: VM with empty ID")
		}
		if vmIDs[r.ID] {
			return fmt.Errorf("vmplacer: duplicate VM ID %q", r.ID)
		}
		vmIDs[r.ID] = true
		if r.CPU < 1 || r.RAM < 1 {
			return fmt.Errorf("vmplacer: VM %q demands must be positive", r.ID)
		}
	}
	return nil
}

// fitsQuota 报告单台 VM 是否满足目标节点的 80% 配额（相对节点总容量，等号允许）。
// 等价于 cpu <= 0.8*CPUTotal 且 ram <= 0.8*RAMTotal，用整数运算避免浮点。
func fitsQuota(r VMRequest, n Node) bool {
	return r.CPU*5 <= 4*n.CPUTotal && r.RAM*5 <= 4*n.RAMTotal
}
