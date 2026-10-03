package vmplanner

// Plan computes a placement for the whole batch on the given snapshot.
// The batch is all-or-nothing: if every request can be assigned to exactly
// one healthy node without exceeding any capacity or per-VM quota, it
// returns a placement whose objective U (the maximum post-placement CPU or
// RAM utilization over all healthy nodes) is the exact global minimum.
// Otherwise it returns Feasible == false.
func Plan(nodes []Node, requests []Request) (PlanResult, error) {
	if err := validateSnapshot(nodes, requests); err != nil {
		return PlanResult{}, err
	}
	return solve(nodes, requests)
}
