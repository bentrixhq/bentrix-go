package mcp

import (
	"encoding/json"
	"testing"
)

// An empty list is sent, and an absent one is not: Market Watch's columns set to none is not Market Watch's columns
// left as they are. A slice left nil is absent; a slice that is empty goes as [] - what every other language's
// client sends.
func TestAnEmptyListIsSentAndAnAbsentOneIsNot(t *testing.T) {
	sent := func(args any) string {
		m, err := plain(args)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(m)
		return string(raw)
	}
	if got := sent(MarketwatchSetViewArgs{Columns: []string{}}); got != `{"columns":[]}` {
		t.Errorf("an empty list was sent as %s", got)
	}
	if got := sent(MarketwatchSetViewArgs{}); got != `{}` {
		t.Errorf("an absent list was sent as %s", got)
	}
	if got := sent(BenchMatrixArgs{Bench: "b", Seeds: []int64{}}); got != `{"bench":"b","seeds":[]}` {
		t.Errorf("an empty list of numbers was sent as %s", got)
	}
}
