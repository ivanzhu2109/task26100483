package vmplacer

import "errors"

// ErrInfeasible 表示整批请求在给定快照上无可行放置。
var ErrInfeasible = errors.New("vmplacer: no feasible placement for the whole batch")

// Result 是 Plan 成功时的返回：完整放置方案及其精确目标值。
type Result struct {
	Placement map[string]string // vmID -> nodeID
	Objective Ratio             // 放置后所有健康节点两种资源利用率的最大值
}

// Plan 在固定快照上为整批 VM 求解精确最优（最小化最大利用率）放置。
// 每台 VM 恰好分到一个健康节点；无可行解时返回 ErrInfeasible。
func Plan(nodes []Node, reqs []VMRequest) (*Result, error) {
	if err := validate(nodes, reqs); err != nil {
		return nil, err
	}
	return solve(nodes, reqs)
}
