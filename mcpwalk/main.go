// The Go MCP client's walk against a running BENTRIX (bentrix --serve over the CI tape): it finds the
// application and pairs with nothing typed, reads its state through the generated surface, reads a
// resource by its template, and runs the same Gym episode every binding runs - through the agents'
// tools, over the wire - printed as JSON for sdk/python/tests/test_crosslang.py.
//
//	LOCALAPPDATA=<the serve's> go run ./mcpwalk
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

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

func main() {
	ctx := context.Background()
	b := must(mcp.Connect(ctx, "go-walk"))
	state := obj(must(b.Workspace.GetState(ctx, mcp.WorkspaceGetStateArgs{})))
	bench := obj(obj(state["laboratories"].([]any)[0])["benches"].([]any)[0])["uri"].(string)
	p := strings.Split(strings.TrimPrefix(bench, "bentrix://"), "/")
	book := obj(must(b.Trade.Book(ctx, mcp.TradeBookArgs{Home: p[0], Owner: p[1], Bench: p[3]})))
	_, isList := book["positions"].([]any)
	check(isList, fmt.Sprint("the book did not read: ", book))
	_, err := b.Call(ctx, "no_such_tool", nil)
	check(err != nil && strings.Contains(err.Error(), "no_such_tool"), fmt.Sprint("a refusal did not say its tool: ", err))

	made := obj(must(b.Strategy.EnvMake(ctx, mcp.StrategyEnvMakeArgs{Bench: bench, Interval: mcp.Ptr("H1"), Window: mcp.Ptr(int64(8)), Episode: mcp.Ptr("window"), EpisodeBars: mcp.Ptr(int64(12))})))
	env := made["env"].(string)
	start := obj(must(b.Strategy.EnvReset(ctx, mcp.StrategyEnvResetArgs{Env: env, Seed: mcp.Ptr(int64(5))})))
	check(len(obj(start["obs"])["market"].([]any)) == 8, "the observation is not shaped as declared")
	info := obj(start["info"])
	rewards := []float64{}
	hashes := []string{info["ledger_hash"].(string)}
	for _, a := range []int{1, 1, 2, 0, 2, 2, 1, 0, 1, 2, 0, 1} {
		s := obj(must(b.Strategy.EnvStep(ctx, mcp.StrategyEnvStepArgs{Env: env, Action: a})))
		rewards = append(rewards, s["reward"].(float64))
		hashes = append(hashes, obj(s["info"])["ledger_hash"].(string))
		if s["terminated"].(bool) || s["truncated"].(bool) {
			break
		}
	}
	must(b.Strategy.EnvClose(ctx, mcp.StrategyEnvCloseArgs{Env: env}))
	// A tool's own refusal (isError), not a request the wire refused.
	_, err = b.Strategy.EnvStep(ctx, mcp.StrategyEnvStepArgs{Env: env, Action: 0})
	var refusal *mcp.ToolError
	check(errors.As(err, &refusal) && strings.Contains(refusal.Msg, "No environment"), fmt.Sprint("a closed environment's step was not refused in its words: ", err))
	distinct := map[string]bool{}
	for _, h := range hashes {
		distinct[h] = true
	}
	check(len(distinct) > 1, "the episode traded nothing")
	// encoding/json writes a float64 in the shortest digits that read back as the same double.
	out, _ := json.Marshal(map[string]any{"start_ms": int64(info["start_ms"].(float64)), "rewards": rewards, "hashes": hashes})
	fmt.Println(string(out))
	fmt.Println("PASS")
}
