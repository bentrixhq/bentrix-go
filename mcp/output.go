// output.go: a model's output of any kind on the chart — the output_spec document built by hand for each of
// the twelve kinds (path, scalar, classes, quantiles, rank, position, weights, event, levels, action, text, series),
// checked here as Bentrix checks it (a value outside its declared range is refused in words, never clamped), put
// through output_put; and the person's meaning (output_meaning: what a high and a low value mean, the thresholds)
// set through output_meaning_set — never in a model's file. An output never names its arrival: Bentrix stamps put_ms.
package mcp

import (
	"context"
	"fmt"
	"math"
	"regexp"
)

// The twelve kinds, as the documents name them.
var OutputKinds = []string{"path", "scalar", "classes", "quantiles", "rank", "position", "weights", "event", "levels", "action", "text", "series"}

// MeaningWords are the product's own words a meaning may use; no profit, win, earn or signal is one.
var MeaningWords = []string{"buy", "sell", "nothing", "up", "down", "flat", "in", "out", "close"}

var label = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// Output is the output_spec document. Values holds one entry per step in the kind's shape.
type Output struct {
	Model, Name, Symbol, Kind string
	AnchorMs                  int64
	Range                     *[2]float64
	Units                     string
	Horizon                   map[string]any
	Values                    []map[string]any
	Names                     []string
	Levels                    []float64
	Universe                  int
	Markets                   []string
	LongOnly, Exclusive       bool
	Softmax, Persistent       bool
	Gross                     float64
	Epoch                     int
	Note                      string
	Suggested                 *Meaning
}

func newOutput(kind, model, name, symbol string, anchorMs int64) *Output {
	return &Output{Model: model, Name: name, Symbol: symbol, Kind: kind, AnchorMs: anchorMs}
}

// Scalar is one number per step, inside rng when given (nil: unbounded).
func Scalar(model, name, symbol string, anchorMs int64, rng *[2]float64, values ...float64) *Output {
	o := newOutput("scalar", model, name, symbol, anchorMs)
	o.Range = rng
	for _, v := range values {
		o.Values = append(o.Values, map[string]any{"value": v})
	}
	return o
}

// Classes is probabilities over named classes, one map per step.
func Classes(model, name, symbol string, anchorMs int64, names []string, exclusive bool, steps ...map[string]float64) *Output {
	o := newOutput("classes", model, name, symbol, anchorMs)
	o.Names, o.Exclusive = names, exclusive
	for _, p := range steps {
		o.Values = append(o.Values, map[string]any{"p": p})
	}
	return o
}

// Quantiles is a value's quantiles per step, keyed by level ("0.1", "0.5", …).
func Quantiles(model, name, symbol string, anchorMs int64, levels []float64, steps ...map[string]float64) *Output {
	o := newOutput("quantiles", model, name, symbol, anchorMs)
	o.Levels = levels
	for _, q := range steps {
		o.Values = append(o.Values, map[string]any{"q": q})
	}
	return o
}

// Rank is a symbol's rank against a universe of universe symbols on a date (Numerai's form), in [0, 1].
func Rank(model, name, symbol string, anchorMs int64, universe int, value float64) *Output {
	o := newOutput("rank", model, name, symbol, anchorMs)
	o.Universe = universe
	o.Values = []map[string]any{{"value": value}}
	return o
}

// Position is a target exposure for the symbol.
func Position(model, name, symbol string, anchorMs int64, units string, target float64) *Output {
	o := newOutput("position", model, name, symbol, anchorMs)
	o.Units = units
	o.Values = []map[string]any{{"target": target}}
	return o
}

// Weights is portfolio weights over markets.
func Weights(model, name, symbol string, anchorMs int64, longOnly bool, w map[string]float64) *Output {
	o := newOutput("weights", model, name, symbol, anchorMs)
	o.LongOnly = longOnly
	for m := range w {
		o.Markets = append(o.Markets, m)
	}
	o.Values = []map[string]any{{"w": w}}
	return o
}

// Event is an event's score in [0, 1], within withinSteps steps when given (0: unsaid).
func Event(model, name, symbol string, anchorMs int64, score float64, withinSteps int) *Output {
	o := newOutput("event", model, name, symbol, anchorMs)
	v := map[string]any{"score": score}
	if withinSteps > 0 {
		v["within_steps"] = withinSteps
	}
	o.Values = []map[string]any{v}
	return o
}

// Levels is named price levels: an entry, a stop, a target.
func Levels(model, name, symbol string, anchorMs int64, levels map[string]float64) *Output {
	o := newOutput("levels", model, name, symbol, anchorMs)
	o.Values = []map[string]any{{"levels": levels}}
	return o
}

// Action is a policy's decision: buy, sell, close_buys, close_sells or nothing; size 0 leaves it unsaid.
func Action(model, name, symbol string, anchorMs int64, intent string, size float64) *Output {
	o := newOutput("action", model, name, symbol, anchorMs)
	v := map[string]any{"intent": intent}
	if size > 0 {
		v["size"] = size
	}
	o.Values = []map[string]any{v}
	return o
}

// Text is a text (an LLM's rationale): on hover only, never on the canvas.
func Text(model, name, symbol string, anchorMs int64, text string) *Output {
	o := newOutput("text", model, name, symbol, anchorMs)
	o.Values = []map[string]any{{"text": text}}
	return o
}

// Series is named lines with no meaning of their own.
func Series(model, name, symbol string, anchorMs int64, names []string, steps ...map[string]float64) *Output {
	o := newOutput("series", model, name, symbol, anchorMs)
	o.Names = names
	for _, l := range steps {
		o.Values = append(o.Values, map[string]any{"lines": l})
	}
	return o
}

// Steps sets the horizon in bars of the anchor's interval.
func (o *Output) Steps(n int) *Output { o.Horizon = map[string]any{"steps": n}; return o }

// BusinessDays sets the horizon in business days (a date-keyed output).
func (o *Output) BusinessDays(n int) *Output { o.Horizon = map[string]any{"business_days": n}; return o }

// Until holds the output until "levels" or "next_output".
func (o *Output) Until(what string) *Output { o.Horizon = map[string]any{"until": what}; return o }

func inRange(field string, v float64, rng *[2]float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("%s %v is not a number", field, v)
	}
	if rng != nil && (v < rng[0] || v > rng[1]) {
		return fmt.Errorf("%s the output answers %v to %v; %v is outside it", field, rng[0], rng[1], v)
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Check refuses what Bentrix would refuse, in its words: the labels, the kind, a value outside its declared range
// (never clamped), the names a kind needs.
func (o *Output) Check() error {
	if !label.MatchString(o.Model) || !label.MatchString(o.Name) {
		return fmt.Errorf("the model and the output are [a-z0-9_]{1,32}")
	}
	if !contains(OutputKinds, o.Kind) {
		return fmt.Errorf("%q is not one of the twelve kinds", o.Kind)
	}
	if len(o.Values) == 0 {
		return fmt.Errorf("values holds no value")
	}
	unit := &[2]float64{0, 1}
	for i, v := range o.Values {
		f := fmt.Sprintf("values[%d]", i)
		switch o.Kind {
		case "scalar":
			if err := inRange(f+".value", toF(v["value"]), o.Range); err != nil {
				return err
			}
		case "rank":
			r := o.Range
			if r == nil {
				r = unit
			}
			if err := inRange(f+".value", toF(v["value"]), r); err != nil {
				return err
			}
		case "event":
			if err := inRange(f+".score", toF(v["score"]), unit); err != nil {
				return err
			}
		case "position":
			if err := inRange(f+".target", toF(v["target"]), o.Range); err != nil {
				return err
			}
		case "classes":
			if len(o.Names) == 0 {
				return fmt.Errorf("names a classes output names its classes")
			}
			for k, p := range v["p"].(map[string]float64) {
				if !contains(o.Names, k) {
					return fmt.Errorf("%s.p %s is not one of the classes", f, k)
				}
				if !o.Softmax {
					if err := inRange(f+".p."+k, p, unit); err != nil {
						return err
					}
				}
			}
		case "quantiles":
			if len(o.Levels) == 0 {
				return fmt.Errorf("levels a quantiles output names its levels")
			}
		case "series":
			if len(o.Names) == 0 {
				return fmt.Errorf("names a series output names its lines")
			}
		case "action":
			if !contains([]string{"buy", "sell", "close_buys", "close_sells", "nothing"}, fmt.Sprint(v["intent"])) {
				return fmt.Errorf("%s.intent %v is not buy, sell, close_buys, close_sells or nothing", f, v["intent"])
			}
		case "text":
			if len([]rune(fmt.Sprint(v["text"]))) > 2000 {
				return fmt.Errorf("%s.text is longer than 2000 characters", f)
			}
		}
	}
	if o.Suggested != nil {
		return o.Suggested.Check(o.Range)
	}
	return nil
}

func toF(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return math.NaN()
}

// Spec is the output_spec document.
func (o *Output) Spec() map[string]any {
	d := map[string]any{"schema_version": 1, "model": o.Model, "output": o.Name, "symbol": o.Symbol, "kind": o.Kind, "anchor": map[string]any{"time_ms": o.AnchorMs}, "values": o.Values}
	if o.Range != nil {
		d["range"] = []float64{o.Range[0], o.Range[1]}
	}
	if o.Units != "" {
		d["units"] = o.Units
	}
	if o.Horizon != nil {
		d["horizon"] = o.Horizon
	}
	if len(o.Names) > 0 {
		d["names"] = o.Names
	}
	if len(o.Levels) > 0 {
		d["levels"] = o.Levels
	}
	if o.Universe > 0 {
		d["universe"] = o.Universe
	}
	if len(o.Markets) > 0 {
		d["markets"] = o.Markets
	}
	if o.Kind == "weights" {
		d["long_only"] = o.LongOnly
	}
	if o.Kind == "classes" {
		d["exclusive"] = o.Exclusive
		if o.Softmax {
			d["softmax"] = true
		}
		if o.Persistent {
			d["persistent"] = true
		}
	}
	if o.Gross > 0 {
		d["gross"] = o.Gross
	}
	if o.Epoch > 0 {
		d["epoch"] = o.Epoch
	}
	if o.Note != "" {
		d["note"] = o.Note
	}
	if o.Suggested != nil {
		d["suggested_meaning"] = o.Suggested.Doc()
	}
	return d
}

// Put is output_put on the workbench bench, after Check.
func (o *Output) Put(ctx context.Context, c Caller, bench string) (any, error) {
	if err := o.Check(); err != nil {
		return nil, err
	}
	return c.Call(ctx, "output_put", map[string]any{"bench": bench, "output": o.Spec()})
}

// PutOutputs is output_put_many: several outputs at one instant.
func PutOutputs(ctx context.Context, c Caller, bench string, os []*Output, allOrNothing bool) (any, error) {
	specs := make([]any, len(os))
	for i, o := range os {
		if err := o.Check(); err != nil {
			return nil, fmt.Errorf("outputs[%d]: %w", i, err)
		}
		specs[i] = o.Spec()
	}
	return c.Call(ctx, "output_put_many", map[string]any{"bench": bench, "outputs": specs, "all_or_nothing": allOrNothing})
}

// Meaning is the output_meaning document: what a value means to the person.
type Meaning struct {
	Reads               string // direction, strength, state, size, level, event or none
	HighMeans, LowMeans string // buy, sell, up, down, nothing, …
	ActAbove, ActBelow  *float64
	Hysteresis          float64
	Labels              map[string]string
}

// Check refuses a word that is not the product's, a threshold outside the output's range, a long label.
func (m *Meaning) Check(rng *[2]float64) error {
	for _, w := range []string{m.HighMeans, m.LowMeans} {
		if w != "" && !contains(MeaningWords, w) {
			return fmt.Errorf("%q is not one of the meaning's words", w)
		}
	}
	for _, t := range []*float64{m.ActAbove, m.ActBelow} {
		if t != nil {
			if err := inRange("", *t, rng); err != nil {
				return fmt.Errorf("the output answers %v to %v; %v is outside it", rng[0], rng[1], *t)
			}
		}
	}
	for k, l := range m.Labels {
		if !contains(MeaningWords, k) || len([]rune(l)) > 24 || l == "" {
			return fmt.Errorf("the label %q for %q is not a meaning word's, or is empty or longer than 24", l, k)
		}
	}
	return nil
}

// Doc is the output_meaning document.
func (m *Meaning) Doc() map[string]any {
	reads := m.Reads
	if reads == "" {
		reads = "direction"
	}
	d := map[string]any{"reads": reads, "hysteresis": m.Hysteresis}
	if m.HighMeans != "" {
		d["high_means"] = m.HighMeans
	}
	if m.LowMeans != "" {
		d["low_means"] = m.LowMeans
	}
	if m.ActAbove != nil {
		d["act_above"] = *m.ActAbove
	}
	if m.ActBelow != nil {
		d["act_below"] = *m.ActBelow
	}
	if len(m.Labels) > 0 {
		d["labels"] = m.Labels
	}
	return d
}

// SetMeaning is output_meaning_set: the person's meaning of model's output on the workbench bench.
func SetMeaning(ctx context.Context, c Caller, bench, model, output string, m *Meaning, rng *[2]float64) (any, error) {
	if err := m.Check(rng); err != nil {
		return nil, err
	}
	return c.Call(ctx, "output_meaning_set", map[string]any{"bench": bench, "model": model, "output": output, "meaning": m.Doc()})
}
