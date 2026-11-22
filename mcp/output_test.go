package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// recorder keeps every call it is asked.
type recorder struct {
	names []string
	args  []map[string]any
}

func (r *recorder) Call(_ context.Context, name string, args any) (any, error) {
	r.names = append(r.names, name)
	r.args = append(r.args, args.(map[string]any))
	return map[string]any{"ok": true}, nil
}

func unit() *[2]float64 { return &[2]float64{0, 1} }
func f(x float64) *float64 { return &x }

// Every kind's builder makes a document Bentrix reads: the twelve, pinned.
func TestEveryKindBuildsADocument(t *testing.T) {
	built := []*Output{
		Scalar("m1", "o1", "EURUSD", 1, unit(), 0.7),
		Classes("m1", "o1", "EURUSD", 1, []string{"up", "down"}, true, map[string]float64{"up": 0.6, "down": 0.4}),
		Quantiles("m1", "o1", "EURUSD", 1, []float64{0.1, 0.9}, map[string]float64{"0.1": 1.0, "0.9": 1.2}),
		Rank("m1", "o1", "AAPL", 1, 4871, 0.62),
		Position("m1", "o1", "EURUSD", 1, "fraction", -0.5),
		Weights("m1", "o1", "EURUSD", 1, true, map[string]float64{"EURUSD": 0.6, "GBPUSD": 0.4}),
		Event("m1", "o1", "EURUSD", 1, 0.8, 3),
		Levels("m1", "o1", "EURUSD", 1, map[string]float64{"stop": 1.09, "target": 1.12}),
		Action("m1", "o1", "EURUSD", 1, "buy", 0.5),
		Text("m1", "o1", "EURUSD", 1, "the trend held"),
		Series("m1", "o1", "EURUSD", 1, []string{"a"}, map[string]float64{"a": 1}),
	}
	kinds := map[string]bool{"path": true} // path is Forecast's (forecast.go)
	for _, o := range built {
		if err := o.Check(); err != nil {
			t.Fatalf("%s: %v", o.Kind, err)
		}
		if _, err := json.Marshal(o.Spec()); err != nil {
			t.Fatalf("%s: %v", o.Kind, err)
		}
		kinds[o.Kind] = true
	}
	if len(kinds) != len(OutputKinds) || len(OutputKinds) != 12 {
		t.Fatalf("the builders cover %d of %d kinds", len(kinds), len(OutputKinds))
	}
}

// A value outside its declared range is refused in words before anything is sent, never clamped.
// Mutation (2026-11-11): inRange not comparing - red, a 1.3 for a 0..1 output was sent.
func TestAValueOutsideItsRangeIsRefusedInWords(t *testing.T) {
	r := &recorder{}
	_, err := Scalar("m1", "o1", "EURUSD", 1, unit(), 1.3).Put(context.Background(), r, "bench")
	if err == nil || !strings.Contains(err.Error(), "0 to 1; 1.3 is outside it") {
		t.Fatalf("a 1.3 for a 0..1 output was sent: %v", err)
	}
	if len(r.names) != 0 {
		t.Fatal("something was sent")
	}
	if Rank("m1", "o1", "AAPL", 1, 10, 1.5).Check() == nil {
		t.Fatal("a rank of 1.5 was taken")
	}
}

// Three examples are three meanings; a word that is not the product's is refused.
func TestTheOwnersExamplesAreMeanings(t *testing.T) {
	r := &recorder{}
	ctx := context.Background()
	for _, m := range []*Meaning{
		{HighMeans: "buy", LowMeans: "sell", ActAbove: f(0.6), ActBelow: f(0.4)},
		{HighMeans: "buy", LowMeans: "nothing", ActAbove: f(0.6)},
		{HighMeans: "sell", LowMeans: "nothing", ActAbove: f(0.6)},
	} {
		if _, err := SetMeaning(ctx, r, "bench", "m1", "o1", m, unit()); err != nil {
			t.Fatal(err)
		}
	}
	if r.names[0] != "output_meaning_set" || r.args[2]["meaning"].(map[string]any)["high_means"] != "sell" {
		t.Fatalf("the meaning was not sent as set: %v", r.args)
	}
	if _, err := SetMeaning(ctx, r, "bench", "m1", "o1", &Meaning{HighMeans: "profit"}, unit()); err == nil {
		t.Fatal("profit was taken as a meaning's word")
	}
	if _, err := SetMeaning(ctx, r, "bench", "m1", "o1", &Meaning{HighMeans: "buy", ActAbove: f(1.5)}, unit()); err == nil {
		t.Fatal("a threshold outside the range was taken")
	}
}

// Put and PutOutputs send output_put and output_put_many with the document; no put_ms is ever sent.
func TestPutSendsTheDocumentAndNeverAnArrival(t *testing.T) {
	r := &recorder{}
	ctx := context.Background()
	if _, err := Scalar("m1", "dir", "EURUSD", 1, unit(), 0.7).Steps(4).Put(ctx, r, "bench"); err != nil {
		t.Fatal(err)
	}
	if _, err := PutOutputs(ctx, r, "bench", []*Output{Event("m1", "e", "EURUSD", 1, 0.9, 0)}, true); err != nil {
		t.Fatal(err)
	}
	spec := r.args[0]["output"].(map[string]any)
	if r.names[0] != "output_put" || spec["kind"] != "scalar" || spec["horizon"].(map[string]any)["steps"] != 4 {
		t.Fatalf("output_put sent %v", r.args[0])
	}
	if _, has := spec["put_ms"]; has {
		t.Fatal("an arrival was sent")
	}
	if r.names[1] != "output_put_many" || r.args[1]["all_or_nothing"] != true {
		t.Fatalf("output_put_many sent %v", r.args[1])
	}
}
