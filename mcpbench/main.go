// The Go Bench's walk against a running Bentrix (bentrix --serve over a practice tape): the common tasks - a refusal
// read as an answer, a step, a buy with a stop and a target, a modify, a part closed, CloseAll, the account and the
// deals - through bench.go, the layer generated from the Bench table. What it saw is printed as JSON for
// sdk/python/tests/test_bench.py, which holds it to the engine's own walk over the same tape.
//
//	LOCALAPPDATA=<the serve's> go run ./mcpbench                  (a running Bentrix)
//	BENTRIX_PRACTICE=<a practice folder> go run ./mcpbench        (the engine in this process: its ws and store)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"bentrix/bentrix"
	"bentrix/mcp"
)

func check(held bool, what string) {
	if !held {
		fmt.Fprintln(os.Stderr, "FAILED:", what)
		os.Exit(1)
	}
}

func must[T any](v T, err error) T {
	check(err == nil, fmt.Sprint(err))
	return v
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

func num(v any) float64 { f, _ := v.(float64); return f }

func main() {
	ctx := context.Background()
	// One walk, either door: the engine in this process over a practice folder, or the running Bentrix.
	var door mcp.Door
	var app *mcp.Bentrix // the running Bentrix's generated capabilities; nil on the engine's door
	if p := os.Getenv("BENTRIX_PRACTICE"); p != "" {
		local := must(bentrix.Open(filepath.Join(p, "ws"), filepath.Join(p, "store")))
		defer local.Close()
		door = local.Door()
	} else {
		app = must(mcp.Connect(ctx, "go-bench"))
		door = app.Client
	}
	bench := must(mcp.OpenBench(ctx, door, "", false))
	markets := must(bench.Markets(ctx))
	check(len(markets) == 1 && markets[0] == "XAUUSD", fmt.Sprint("the markets: ", markets))
	refused := must(bench.Buy(ctx, 0.001, nil))
	check(!refused.Ok() && refused.Kind == "volume_out_of_range", fmt.Sprint("the refusal: ", refused.Raw))
	check(refused.Reason != "" && strings.Contains(refused.Remedy, "volume_min") && strings.HasSuffix(refused.Docs, "go/concept.volume/"), fmt.Sprint("the refusal's words: ", refused.Raw))
	first := must(bench.Step(ctx, &mcp.StepOptions{Interval: "H1"}))
	check(first != nil && num(first["time"]) > 0 && num(first["close"]) > 0, fmt.Sprint("the first step: ", first))
	group := must(bench.Group(ctx, "walk"))
	check(group == 1, fmt.Sprint("a group's number: ", group))
	closed := must(bench.Bars(ctx, &mcp.BarsOptions{Interval: "H1", Limit: mcp.Ptr(int64(5))}))
	check(len(closed) == 1 && num(closed[0].([]any)[0]) == num(first["time"]), fmt.Sprint("the closed candles: ", closed))
	q := must(bench.Quote(ctx, nil))
	check(q != nil && num(q["ask"]) > num(q["bid"]), fmt.Sprint("the quote: ", q))
	sl, tp := math.RoundToEven(num(q["ask"])-30.0), math.RoundToEven(num(q["ask"])+30.0)
	check(must(bench.Buy(ctx, 0.1, &mcp.BuyOptions{Sl: mcp.Ptr(sl), Tp: mcp.Ptr(tp)})).Ok(), "the buy")
	must(bench.Step(ctx, &mcp.StepOptions{Interval: "H1"}))
	positions := must(bench.Positions(ctx))
	check(len(positions) == 1, fmt.Sprint("the positions: ", positions))
	p := obj(positions[0])
	check(p["side"] == "buy" && num(p["volume"]) == 0.1 && num(p["sl"]) == sl && num(p["tp"]) == tp, fmt.Sprint("the position: ", p))
	check(must(bench.Modify(ctx, p, &mcp.ModifyOptions{Sl: mcp.Ptr(sl + 5.0)})).Ok(), "the modify")
	moved := obj(must(bench.Positions(ctx))[0])
	check(num(moved["sl"]) == sl+5.0 && num(moved["tp"]) == tp, fmt.Sprint("only what was named changes: ", moved))
	check(must(bench.Close(ctx, moved, &mcp.CloseOptions{Lots: mcp.Ptr(0.04)})).Ok(), "the part closed")
	must(bench.Step(ctx, &mcp.StepOptions{Interval: "H1"}))
	rest := obj(must(bench.Positions(ctx))[0])
	check(num(rest["volume"]) == 0.06, fmt.Sprint("the rest: ", rest))
	check(must(bench.CloseAll(ctx, nil)).Ok(), "CloseAll")
	must(bench.Step(ctx, &mcp.StepOptions{Interval: "H1"}))
	check(len(must(bench.Positions(ctx))) == 0, "a position stayed")
	nothing := must(bench.CloseAll(ctx, nil))
	check(nothing.Ok() && num(nothing.Raw["closed"]) == 0 && nothing.Raw["closed"] != nil, fmt.Sprint("CloseAll with nothing open: ", nothing.Raw))
	account := obj(must(bench.Account(ctx)))
	check(num(account["equity"]) == num(account["balance"]) && num(account["balance"]) != 10000.0, fmt.Sprint("the account: ", account))
	deals := must(bench.Deals(ctx))
	ins, outs := 0, 0
	seenDeals := [][]any{}
	for _, d := range deals {
		m := obj(d)
		switch m["entry"] {
		case "in":
			ins++
		case "out":
			outs++
		}
		seenDeals = append(seenDeals, []any{m["entry"], m["side"], m["volume"]})
	}
	check(ins == 1 && outs == 2, fmt.Sprint("the deals: ", deals))
	kept := must(bench.Keep(ctx, "kept"))
	check(len(must(kept.Positions(ctx))) == 0, "the kept workbench reads")
	// Past the days' end, advance stands at their last moment, by either door.
	end := must(bench.Advance(ctx, int64(4102444800000)))
	check(end > 0 && end < 4102444800000, fmt.Sprint("the days' end: ", end))
	if app != nil {
		// A workbench resource by its address: the same book as by its three parts.
		p := strings.Split(strings.TrimPrefix(bench.URI, "bentrix://"), "/")
		byParts := must(json.Marshal(must(app.Trade.Book(ctx, mcp.TradeBookArgs{Home: p[0], Owner: p[1], Bench: p[3]}))))
		byAddress := must(json.Marshal(must(app.Trade.Book(ctx, mcp.TradeBookArgs{Bench: bench.URI}))))
		check(string(byAddress) == string(byParts), fmt.Sprint("the book by its address: ", string(byAddress)))
	}
	seen := map[string]any{
		"refused": []any{refused.Outcome, refused.Kind, refused.Reason},
		"first":   first,
		"group":   group,
		"bars":    []any{closed[0].([]any)[0]},
		"deals":   seenDeals,
		"balance": account["balance"],
		"end":     end,
	}
	out, _ := json.Marshal(seen)
	fmt.Println(string(out))
	fmt.Println("PASS")
}
