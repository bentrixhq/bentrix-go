package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// sheet answers wait_for on the eval event with scripted pages.
type sheet struct {
	pages [][]any
	asked []string
}

func (s *sheet) Call(_ context.Context, name string, args any) (any, error) {
	a := args.(map[string]any)
	if name != "wait_for" || a["uri"] != EvalEvent {
		return nil, errors.New("not the eval event: " + name)
	}
	after := a["after"].(string)
	s.asked = append(s.asked, after)
	var items []any
	if len(s.pages) > 0 {
		items, s.pages = s.pages[0], s.pages[1:]
	}
	n := 0
	_ = json.Unmarshal([]byte(after), &n)
	return map[string]any{"items": items, "next": func() string { b, _ := json.Marshal(n + len(items)); return string(b) }()}, nil
}

func move(ask, state string, kv ...any) map[string]any {
	m := map[string]any{"ask": ask, "state": state}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// An element forecast names no interval and carries its rule; a time forecast names its interval.
// Mutation (2026-11-08): Spec writing timeframe whatever it is — red, the element form named an interval.
func TestAnElementForecastNamesNoInterval(t *testing.T) {
	at := int64(1_700_000_600_000)
	f := NewElementForecast("bricks", "EURUSD", "renko", 1_700_000_000_000, map[string]any{"renko_box": 0.0005, "renko_reversal": 2})
	f.Step(Step{Close: 1.1005, ExpectedMs: &at})
	form := f.Spec()["form"].(map[string]any)
	if _, named := form["timeframe"]; named || form["chart_type"] != "renko" || form["elements"] == nil {
		t.Fatalf("the element form: %v", form)
	}
	steps := f.Spec()["steps"].([]any)
	if steps[0].(map[string]any)["expected_ms"] == nil {
		t.Fatalf("a step lost its expected_ms: %v", steps)
	}
	tf := NewForecast("m", "EURUSD", "H1", 0).Step(Step{Close: 1}).Spec()["form"]
	if !reflect.DeepEqual(tf, map[string]any{"chart_type": "candlestick", "timeframe": "H1"}) {
		t.Fatalf("the time form: %v", tf)
	}
}

// The run, the decline and the failure are three answers; another ask's moves are skipped.
// Mutation (2026-11-08): declined read as a failure — red, a decline was EvalFailed.
func TestAnEvalsSheetIsWaitedOnAndADeclineIsNotAFailure(t *testing.T) {
	ctx := context.Background()
	if e, err := EvalStarted(ctx, &sheet{}, map[string]any{"eval": float64(7)}, 0, 1); e != 7 || err != nil {
		t.Fatalf("a run at once: %v %v", e, err)
	}
	ask := map[string]any{"ask": "bentrix://ask/2", "watch": "bentrix://forecasts/eval?after=0"}
	s := &sheet{pages: [][]any{{move("bentrix://ask/1", "declined"), move("bentrix://ask/2", "asked")}, {move("bentrix://ask/2", "made_laboratory")}, {move("bentrix://ask/2", "started", "eval", float64(11))}}}
	if e, err := EvalStarted(ctx, s, ask, 0, 3); e != 11 || err != nil {
		t.Fatalf("the run the person started: %v %v", e, err)
	}
	if !reflect.DeepEqual(s.asked, []string{"0", "2", "3"}) {
		t.Fatalf("the cursor: %v", s.asked)
	}
	_, err := EvalStarted(ctx, &sheet{pages: [][]any{{move("bentrix://ask/2", "declined", "by", "the person")}}}, ask, 0, 3)
	var no *DeclinedByThePerson
	if !errors.As(err, &no) {
		t.Fatalf("a decline: %v", err)
	}
	_, err = EvalStarted(ctx, &sheet{pages: [][]any{{move("bentrix://ask/2", "failed", "why", "the laboratory was not made")}}}, ask, 0, 3)
	var failed *EvalFailed
	if !errors.As(err, &failed) || failed.Why != "the laboratory was not made" {
		t.Fatalf("a failure: %v", err)
	}
	if _, err = EvalStarted(ctx, &sheet{}, ask, 0, 2); err == nil {
		t.Fatal("a watch with no answer did not end")
	}
}
