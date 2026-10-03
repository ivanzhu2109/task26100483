// Package vmplanner implements an exact batch VM placement planner for
// edge clusters. It works on a fixed snapshot: it neither executes VM
// operations nor handles persistence or concurrent commits.
package vmplanner

// Node describes one cluster node at a fixed snapshot.
type Node struct {
	ID      string
	Healthy bool
	// TotalCPU in whole cores (<= 64), TotalRAM in MiB (<= 524288).
	TotalCPU int64
	TotalRAM int64
	// Existing usage, guaranteed not to exceed capacity.
	UsedCPU int64
	UsedRAM int64
}

// Request describes one VM to place.
type Request struct {
	ID  string
	CPU int64 // whole cores, positive
	RAM int64 // MiB, positive
}

// Placement maps request ID to node ID.
type Placement map[string]string

// Ratio is an exact non-negative rational Num/Den, always normalized
// (Den > 0, gcd(|Num|, Den) == 1). Comparisons never use floats.
type Ratio struct {
	Num int64
	Den int64
}

// PlanResult is the outcome of Plan.
type PlanResult struct {
	// Feasible is true iff the whole batch can be placed.
	Feasible bool
	// Placement assigns every request to exactly one node (Feasible only).
	Placement Placement
	// Objective is the exact globally minimal max-utilization U
	// (Feasible only).
	Objective Ratio
}

// CheckResult is the outcome of Check.
type CheckResult struct {
	// Valid is true iff the placement is complete and respects all rules.
	Valid bool
	// Reason explains the first violation found (Valid == false only).
	Reason string
	// Objective is the exact max-utilization U of the placement,
	// computed with the same definition as Plan (Valid only).
	Objective Ratio
}
