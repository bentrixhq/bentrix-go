// forecast.go: a model's forecast on the chart, built by hand — the forecast_spec document step by step,
// a time form (an interval) or an element form (a chart built by price: renko, kagi, line_break, point_figure,
// range_bars — no interval, its rule in form.elements), put through forecast_put; and an evaluation's sheet waited
// on: forecast_eval_make's answer is the run at once, or an ask the person answers in Bentrix's window, watched on
// the bentrix://forecasts/eval event. A forecast never names its arrival: Bentrix stamps put_ms.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Caller is what a forecast is put through: *Client, or a test's script.
type Caller interface {
	Call(ctx context.Context, name string, args any) (any, error)
}

// Step is one bar of a forecast. Close is required; the rest are left out when nil or empty.
type Step struct {
	Close      float64            `json:"close"`
	Open       *float64           `json:"open,omitempty"`
	High       *float64           `json:"high,omitempty"`
	Low        *float64           `json:"low,omitempty"`
	Volume     *float64           `json:"volume,omitempty"`
	Bands      []Band             `json:"bands,omitempty"`
	Quantiles  map[string]float64 `json:"quantiles,omitempty"`
	Samples    []float64          `json:"samples,omitempty"`
	ExpectedMs *int64             `json:"expected_ms,omitempty"`
}

// Band is a range a step is expected within, at probability P when given.
type Band struct {
	Lo float64  `json:"lo"`
	Hi float64  `json:"hi"`
	P  *float64 `json:"p,omitempty"`
}

// Forecast is the forecast_spec document. AnchorMs is the open of the last closed bar (or the moment of the last
// closed element) it is made at; step 1 is the one after it.
type Forecast struct {
	Model     string
	Symbol    string
	ChartType string
	Timeframe string         // a time form's interval; "" for an element form
	Elements  map[string]any // an element form's rule, where it is not the chart's own
	AnchorMs  int64
	Epoch     int
	Note      string
	Look      map[string]string
	Steps     []Step
}

// NewForecast is a forecast of an interval's bars, drawn as candles.
func NewForecast(model, symbol, timeframe string, anchorMs int64) *Forecast {
	return &Forecast{Model: model, Symbol: symbol, ChartType: "candlestick", Timeframe: timeframe, AnchorMs: anchorMs}
}

// NewElementForecast is a forecast of a chart built by price: no interval, elements its rule (nil for the chart's
// own). A step's ExpectedMs, given on every step or none, is what places it on a chart by time.
func NewElementForecast(model, symbol, chartType string, anchorMs int64, elements map[string]any) *Forecast {
	return &Forecast{Model: model, Symbol: symbol, ChartType: chartType, Elements: elements, AnchorMs: anchorMs}
}

// Step adds one bar and answers the forecast.
func (f *Forecast) Step(s Step) *Forecast {
	f.Steps = append(f.Steps, s)
	return f
}

// Spec is the forecast_spec document.
func (f *Forecast) Spec() map[string]any {
	form := map[string]any{"chart_type": f.ChartType}
	if f.Timeframe != "" {
		form["timeframe"] = f.Timeframe
	}
	if f.Elements != nil {
		form["elements"] = f.Elements
	}
	steps := make([]any, 0, len(f.Steps))
	for _, s := range f.Steps {
		var m map[string]any
		b, _ := json.Marshal(s)
		_ = json.Unmarshal(b, &m)
		steps = append(steps, m)
	}
	d := map[string]any{"schema_version": 1, "model": f.Model, "symbol": f.Symbol, "form": form,
		"anchor": map[string]any{"time_ms": f.AnchorMs}, "steps": steps, "epoch": f.Epoch}
	if f.Note != "" {
		d["note"] = f.Note
	}
	if f.Look != nil {
		d["look"] = f.Look
	}
	return d
}

// Put is forecast_put on the workbench bench: its arrival, its state, where it stands on each chart.
func (f *Forecast) Put(ctx context.Context, c Caller, bench string) (any, error) {
	return c.Call(ctx, "forecast_put", map[string]any{"bench": bench, "forecast": f.Spec()})
}

// PutMany is forecast_put_many: several markets and models at one instant.
func PutMany(ctx context.Context, c Caller, bench string, fs []*Forecast, allOrNothing bool) (any, error) {
	specs := make([]any, len(fs))
	for i, f := range fs {
		specs[i] = f.Spec()
	}
	return c.Call(ctx, "forecast_put_many", map[string]any{"bench": bench, "forecasts": specs, "all_or_nothing": allOrNothing})
}

// EvalEvent is the event an ask's moves are carried on.
const EvalEvent = "bentrix://forecasts/eval"

// DeclinedByThePerson is the person's No on the evaluation's sheet. Not a failure: nothing was made or fetched.
type DeclinedByThePerson struct{ Ask string }

func (e *DeclinedByThePerson) Error() string { return "the person declined " + e.Ask }

// EvalFailed is a laboratory not made over the span, or a run that did not start on it, with Bentrix's why.
type EvalFailed struct{ Ask, Why string }

func (e *EvalFailed) Error() string { return e.Why }

// EvalStarted is the run to drive, from forecast_eval_make's answer: its eval at once, or — over days the
// workbench's laboratory does not hold — the person's answer on the sheet, watched on EvalEvent. started answers the
// run; declined is *DeclinedByThePerson; failed is *EvalFailed. No tool answers the sheet. idleLimit waits in a row
// with no move of this ask end the watch with an error (never, when 0).
func EvalStarted(ctx context.Context, c Caller, made map[string]any, timeoutMs int, idleLimit int) (int64, error) {
	if e, ok := made["eval"]; ok && e != nil {
		return toInt(e), nil
	}
	ask, _ := made["ask"].(string)
	uri, _ := made["watch"].(string)
	if uri == "" {
		uri = EvalEvent
	}
	cursor := "0"
	if i := strings.Index(uri, "after="); i >= 0 {
		cursor = strings.SplitN(uri[i+len("after="):], "&", 2)[0]
	}
	uri = strings.SplitN(strings.SplitN(uri, "{", 2)[0], "?", 2)[0]
	for idle := 0; idleLimit == 0 || idle < idleLimit; {
		got, err := c.Call(ctx, "wait_for", map[string]any{"uri": uri, "after": cursor, "timeout_ms": timeoutMs})
		if err != nil {
			return 0, err
		}
		page, _ := got.(map[string]any)
		if n, ok := page["next"].(string); ok {
			cursor = n
		}
		moved := false
		items, _ := page["items"].([]any)
		for _, it := range items {
			item, _ := it.(map[string]any)
			if item["ask"] != ask {
				continue
			}
			moved = true
			switch item["state"] {
			case "started":
				return toInt(item["eval"]), nil
			case "declined":
				return 0, &DeclinedByThePerson{Ask: ask}
			case "failed":
				why, _ := item["why"].(string)
				if why == "" {
					why = "the evaluation did not start"
				}
				return 0, &EvalFailed{Ask: ask, Why: why}
			}
		}
		if moved {
			idle = 0
		} else {
			idle++
		}
	}
	return 0, fmt.Errorf("%s: no answer on the sheet", ask)
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
