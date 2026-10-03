package vmplanner

import "testing"

// Heterogeneous nodes where placing a flexible VM on the wrong node blocks
// a VM that can only go to one specific node, yet the batch is feasible.
func TestPlanFlexibleVMMustNotBlockConstrainedVM(t *testing.T) {
	nodes := []Node{
		{ID: "big", Healthy: true, TotalCPU: 10, TotalRAM: 10240, UsedCPU: 5},
		{ID: "small", Healthy: true, TotalCPU: 4, TotalRAM: 10240},
	}
	reqs := []Request{
		{ID: "flex", CPU: 3, RAM: 1024},  // fits both nodes
		{ID: "rigid", CPU: 5, RAM: 1024}, // quota: only "big" (8 >= 5; small allows 3)
	}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Feasible {
		t.Fatal("batch is feasible: flex->small, rigid->big")
	}
	if res.Placement["rigid"] != "big" || res.Placement["flex"] != "small" {
		t.Fatalf("unexpected placement: %v", res.Placement)
	}
	chk, err := Check(nodes, reqs, res.Placement)
	if err != nil || !chk.Valid {
		t.Fatalf("Check rejected Plan output: %v %v", err, chk.Reason)
	}
}

// Per-VM quota boundary: demand == 80% of TOTAL capacity is allowed.
func TestQuotaEqualityAllowed(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, TotalCPU: 10, TotalRAM: 1000},
		{ID: "b", Healthy: true, TotalCPU: 10, TotalRAM: 1000},
	}
	reqs := []Request{{ID: "vm", CPU: 8, RAM: 800}} // exactly 80% of totals
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Feasible {
		t.Fatal("demand equal to 80% of total capacity must be accepted")
	}
}

// Just over the quota: 80% + epsilon must be rejected.
func TestQuotaJustOverRejected(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, TotalCPU: 10, TotalRAM: 1000},
		{ID: "b", Healthy: true, TotalCPU: 10, TotalRAM: 1000},
	}
	// 9 > 8 CPU quota; 801 > 800 RAM quota. Either alone must sink the batch.
	for _, r := range []Request{
		{ID: "vm", CPU: 9, RAM: 800},
		{ID: "vm", CPU: 8, RAM: 801},
	} {
		res, err := Plan(nodes, []Request{r})
		if err != nil {
			t.Fatal(err)
		}
		if res.Feasible {
			t.Fatalf("demand %+v exceeds the 80%% quota, must be infeasible", r)
		}
	}
}

// The quota is 80% of TOTAL capacity, not of the remaining capacity.
// A VM above 80% of the remainder but within 80% of the total must place.
func TestQuotaIsOnTotalNotRemaining(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, TotalCPU: 10, TotalRAM: 10240, UsedCPU: 3},
		{ID: "b", Healthy: true, TotalCPU: 10, TotalRAM: 10240, UsedCPU: 9},
	}
	// Node a: remaining 7, 80% of remaining would be 5.6 -> 6 CPU would be
	// wrongly rejected. Correct quota is 8 (80% of total), so 6 is fine.
	reqs := []Request{{ID: "vm", CPU: 6, RAM: 1024}}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Feasible {
		t.Fatal("quota misread as 80% of remaining capacity gives a wrong infeasible verdict")
	}
	if res.Placement["vm"] != "a" {
		t.Fatalf("expected placement on node a, got %v", res.Placement)
	}
	// And 7 CPU fits neither quota-correctly-on-a (7<=8 ok, remaining 7 ok)
	// ... verify the exact boundary: 8 CPU passes quota but not remaining (7).
	res2, err := Plan(nodes, []Request{{ID: "vm", CPU: 8, RAM: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Feasible {
		t.Fatal("8 CPU passes the quota but exceeds remaining capacity 7 on a and 1 on b")
	}
}

// All-or-nothing: one unplaceable VM sinks the whole batch.
func TestBatchAllOrNothing(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, TotalCPU: 4, TotalRAM: 4096},
		{ID: "b", Healthy: true, TotalCPU: 4, TotalRAM: 4096},
	}
	reqs := []Request{
		{ID: "ok", CPU: 1, RAM: 1024},
		{ID: "huge", CPU: 64, RAM: 1024}, // exceeds every quota
	}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if res.Feasible {
		t.Fatal("batch containing an unplaceable VM must be rejected as a whole")
	}
}

// Unhealthy nodes never take new VMs and never count toward U.
func TestUnhealthyNodesExcluded(t *testing.T) {
	nodes := []Node{
		{ID: "sick", Healthy: false, TotalCPU: 64, TotalRAM: 524288, UsedCPU: 64, UsedRAM: 524288},
		{ID: "well", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
		{ID: "well2", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
	}
	reqs := []Request{{ID: "vm", CPU: 2, RAM: 2048}}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Feasible {
		t.Fatal("expected feasible")
	}
	if res.Placement["vm"] == "sick" {
		t.Fatal("unhealthy node received a VM")
	}
	// U must ignore the sick node's 100% utilization: 2/8 = 1/4.
	if res.Objective != (Ratio{Num: 1, Den: 4}) {
		t.Fatalf("objective = %v, want 1/4", res.Objective)
	}
}

// Exact rational objective: 1/3 must be reported as 1/3, not a float.
func TestObjectiveExactRatio(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, TotalCPU: 6, TotalRAM: 6144},
		{ID: "b", Healthy: true, TotalCPU: 6, TotalRAM: 6144},
	}
	reqs := []Request{
		{ID: "vm1", CPU: 1, RAM: 1024},
		{ID: "vm2", CPU: 1, RAM: 1024},
	}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Feasible {
		t.Fatal("expected feasible")
	}
	// Optimal: one VM per node -> U = 1/6 exactly.
	if res.Objective != (Ratio{Num: 1, Den: 6}) {
		t.Fatalf("objective = %v, want 1/6", res.Objective)
	}
}

func TestCheckValidatesCallerPlacements(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
		{ID: "b", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
	}
	reqs := []Request{
		{ID: "vm1", CPU: 4, RAM: 4096},
		{ID: "vm2", CPU: 4, RAM: 4096},
	}
	cases := []struct {
		name      string
		placement Placement
		valid     bool
	}{
		{"complete valid", Placement{"vm1": "a", "vm2": "b"}, true},
		{"valid but suboptimal", Placement{"vm1": "a", "vm2": "a"}, true}, // Check ignores optimality
		{"missing vm", Placement{"vm1": "a"}, false},
		{"unknown node", Placement{"vm1": "a", "vm2": "zzz"}, false},
		{"unknown vm key", Placement{"vm1": "a", "vm2": "b", "vm3": "a"}, false},
		{"overcommit", Placement{"vm1": "a", "vm2": "a"}, true},
	}
	for _, tc := range cases {
		res, err := Check(nodes, reqs, tc.placement)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
		if res.Valid != tc.valid {
			t.Fatalf("%s: valid=%v, want %v (reason %q)", tc.name, res.Valid, tc.valid, res.Reason)
		}
	}

	// Overcommit: two 5-CPU VMs on one 8-CPU node.
	big := []Request{{ID: "x", CPU: 5, RAM: 1024}, {ID: "y", CPU: 5, RAM: 1024}}
	res, err := Check(nodes, big, Placement{"x": "a", "y": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatal("expected CPU overcommit to be rejected")
	}

	// Quota violation caught by Check: 7 CPU on an 8-CPU node (quota 6.4).
	res, err = Check(nodes, []Request{{ID: "q", CPU: 7, RAM: 1024}}, Placement{"q": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatal("expected per-VM quota violation to be rejected")
	}

	// Unhealthy target caught by Check.
	sick := append([]Node(nil), nodes...)
	sick[0].Healthy = false
	res, err = Check(sick, reqs, Placement{"vm1": "a", "vm2": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid {
		t.Fatal("expected placement on unhealthy node to be rejected")
	}

	// Objective from Check matches the same definition as Plan:
	// both 4-CPU VMs on one 8-CPU node saturate it -> U = 1.
	res, err = Check(nodes, reqs, Placement{"vm1": "a", "vm2": "a"})
	if err != nil || !res.Valid {
		t.Fatal(err)
	}
	if res.Objective != (Ratio{Num: 1, Den: 1}) {
		t.Fatalf("objective = %v, want 1/1", res.Objective)
	}
}

func TestInputValidation(t *testing.T) {
	good := []Node{
		{ID: "a", Healthy: true, TotalCPU: 4, TotalRAM: 4096},
		{ID: "b", Healthy: true, TotalCPU: 4, TotalRAM: 4096},
	}
	one := []Request{{ID: "vm", CPU: 1, RAM: 1024}}
	if _, err := Plan([]Node{good[0]}, one); err == nil {
		t.Fatal("expected error for a single node")
	}
	if _, err := Plan(append(good, good...), one); err == nil {
		t.Fatal("expected error for duplicate node IDs")
	}
	if _, err := Plan(good, []Request{{ID: "v", CPU: 0, RAM: 1}}); err == nil {
		t.Fatal("expected error for non-positive demand")
	}
	if _, err := Plan(good, append(one, one...)); err == nil {
		t.Fatal("expected error for duplicate request IDs")
	}
	badUsed := []Node{
		{ID: "a", Healthy: true, TotalCPU: 4, TotalRAM: 4096, UsedCPU: 5},
		good[1],
	}
	if _, err := Plan(badUsed, one); err == nil {
		t.Fatal("expected error for existing usage above capacity")
	}
}
