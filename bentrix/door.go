package bentrix

import (
	"context"

	"bentrix/mcp"
)

// Door is this engine as a door of a workbench object (mcp.OpenBench): the same Bench over the engine in this process
// as over a running Bentrix.
//
//	local, _ := bentrix.Open(workspace, store)
//	bench, _ := mcp.OpenBench(ctx, local.Door(), "", false)
func (l *Local) Door() mcp.Door { return localDoor{l} }

type localDoor struct{ l *Local }

func (d localDoor) Call(_ context.Context, name string, args any) (any, error) {
	got, err := d.l.Call(name, args)
	if err != nil {
		return nil, err
	}
	return got, nil
}
