package vmplanner

import "testing"

func TestSmoke(t *testing.T) {
	nodes := []Node{
		{ID: "a", Healthy: true, TotalCPU: 8, TotalRAM: 8192},
		{ID: "b", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
	}
	reqs := []Request{
		{ID: "vm1", CPU: 4, RAM: 4096},
		{ID: "vm2", CPU: 8, RAM: 8192},
	}
	res, err := Plan(nodes, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Feasible {
		t.Fatal("expected feasible")
	}
	chk, err := Check(nodes, reqs, res.Placement)
	if err != nil || !chk.Valid {
		t.Fatalf("check failed: %v %v", err, chk.Reason)
	}
	if chk.Objective != res.Objective {
		t.Fatalf("objective mismatch: %v vs %v", chk.Objective, res.Objective)
	}
	t.Logf("objective U = %d/%d ≈ %.4f", res.Objective.Num, res.Objective.Den, res.Objective.Float64())
}
