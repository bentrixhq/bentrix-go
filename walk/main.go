// The Go binding's walk on the CI tape: the engine (a second session refused, thirty hours advanced, a
// market order filled, the book read, a refusal in its words) and the Gym episode every binding runs,
// printed as JSON for sdk/python/tests/test_crosslang.py.
//
//	CGO_LDFLAGS=-L<dir> go run ./walk <empty directory>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"bentrix/bentrix"
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

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: walk <empty directory>")
		os.Exit(2)
	}
	fmt.Println("bentrix", bentrix.Version())
	ws, store, err := bentrix.MakeTape(os.Args[1])
	check(err == nil, fmt.Sprint(err))
	local := must(bentrix.Open(ws, store))
	defer local.Close()
	_, err = bentrix.Open(ws, store)
	check(err != nil, "a second session on the held workspace was not refused")
	fmt.Println("a second session refused:", err)
	state := must(local.Call("get_state", nil))
	bench := state["laboratories"].([]any)[0].(map[string]any)["benches"].([]any)[0].(map[string]any)["uri"].(string)
	check(local.Arm(bench) == nil, "the workbench did not arm")
	from := int64(must(local.Call("replay_step", map[string]any{"bench": bench, "unit": "tick", "n": 1}))["cursor_ms"].(float64))
	reached := must(local.Advance(bench, from+30*3600000))
	check(reached >= from+30*3600000, "the workbench did not advance")
	must(local.Call("order_market", map[string]any{"bench": bench, "symbol": "XAUUSD", "side": "buy", "volume": 0.01}))
	reached = must(local.Advance(bench, reached+60000))
	positions := must(local.Book(bench))["positions"].([]any)
	check(len(positions) == 1 && positions[0].(map[string]any)["side"] == "buy", "the order did not fill")
	_, err = local.Call("no_such_tool", nil)
	check(err != nil && strings.Contains(err.Error(), "no_such_tool"), "a refusal did not say its tool")
	check(bentrix.IntervalMs("H1") == 3600000, "H1 is not an hour")

	env := must(bentrix.NewTradingEnv(local, bench, bentrix.EnvOptions{Interval: "H1", Window: 8, Episode: "window", EpisodeBars: 12}))
	seed := uint64(5)
	obs, info, err := env.Reset(&seed)
	check(err == nil && len(obs["market"].([]any)) == 8, "the observation is not shaped as declared")
	rewards := []float64{}
	hashes := []string{info["ledger_hash"].(string)}
	for _, a := range []int{1, 1, 2, 0, 2, 2, 1, 0, 1, 2, 0, 1} {
		s := must(env.Step(a))
		rewards = append(rewards, s.Reward)
		hashes = append(hashes, s.Info["ledger_hash"].(string))
		if s.Terminated || s.Truncated {
			break
		}
	}
	env.Close()
	out, _ := json.Marshal(map[string]any{"start_ms": int64(info["start_ms"].(float64)), "rewards": rewards, "hashes": hashes})
	fmt.Println(string(out))
	fmt.Println("PASS")
}
