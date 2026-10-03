package vmplanner_test

import (
	"fmt"

	"vmplanner"
)

func ExamplePlan() {
	nodes := []vmplanner.Node{
		{ID: "edge-1", Healthy: true, TotalCPU: 8, TotalRAM: 8192, UsedCPU: 2, UsedRAM: 2048},
		{ID: "edge-2", Healthy: true, TotalCPU: 16, TotalRAM: 16384},
		{ID: "edge-3", Healthy: false, TotalCPU: 32, TotalRAM: 65536}, // unhealthy: excluded
	}
	reqs := []vmplanner.Request{
		{ID: "vm-a", CPU: 4, RAM: 4096},
		{ID: "vm-b", CPU: 6, RAM: 6144},
	}
	res, err := vmplanner.Plan(nodes, reqs)
	if err != nil {
		panic(err)
	}
	if !res.Feasible {
		fmt.Println("batch rejected: does not fit as a whole")
		return
	}
	chk, err := vmplanner.Check(nodes, reqs, res.Placement)
	if err != nil {
		panic(err)
	}
	fmt.Printf("placement=%v valid=%v U=%d/%d (%.2f%%)\n",
		res.Placement, chk.Valid, res.Objective.Num, res.Objective.Den,
		100*res.Objective.Float64())
	// Output:
	// placement=map[vm-a:edge-2 vm-b:edge-2] valid=true U=5/8 (62.50%)
}
