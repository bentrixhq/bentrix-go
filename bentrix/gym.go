package bentrix

// The Gym: a client of BENTRIX's own environment. Reset(seed) answers the observation and its info;
// Step(action) the observation, the reward, terminated, truncated and the info - Gymnasium's shape.

// EnvOptions are env_make's arguments; a zero value leaves the environment's default.
type EnvOptions struct {
	Symbol, Interval, Action, Reward, Episode string
	Window, EpisodeBars                       int
	Lots                                      float64
}

// TradingEnv is an environment over a workbench.
type TradingEnv struct {
	local  *Local
	ID     string
	Symbol string
}

// Step is one step's answer.
type Step struct {
	Obs        map[string]any
	Reward     float64
	Terminated bool
	Truncated  bool
	Info       map[string]any
}

func put(m map[string]any, k string, v any, zero bool) {
	if !zero {
		m[k] = v
	}
}

// NewTradingEnv makes an environment over bench. A laboratory workbench is rewound at each reset, its
// deals undone: give it one of its own.
func NewTradingEnv(l *Local, bench string, o EnvOptions) (*TradingEnv, error) {
	args := map[string]any{"bench": bench}
	put(args, "symbol", o.Symbol, o.Symbol == "")
	put(args, "interval", o.Interval, o.Interval == "")
	put(args, "action", o.Action, o.Action == "")
	put(args, "reward", o.Reward, o.Reward == "")
	put(args, "episode", o.Episode, o.Episode == "")
	put(args, "window", o.Window, o.Window == 0)
	put(args, "episode_bars", o.EpisodeBars, o.EpisodeBars == 0)
	put(args, "lots", o.Lots, o.Lots == 0)
	made, err := l.Call("env_make", args)
	if err != nil {
		return nil, err
	}
	return &TradingEnv{local: l, ID: made["env"].(string), Symbol: made["symbol"].(string)}, nil
}

// Reset starts an episode; a seed given starts the draws over (nil: the next episode).
func (e *TradingEnv) Reset(seed *uint64) (map[string]any, map[string]any, error) {
	args := map[string]any{"env": e.ID}
	if seed != nil {
		args["seed"] = *seed
	}
	got, err := e.local.Call("env_reset", args)
	if err != nil {
		return nil, nil, err
	}
	return got["obs"].(map[string]any), got["info"].(map[string]any), nil
}

// Step takes one action: 0, 1 or 2 (discrete), a number (continuous) or a map (order).
func (e *TradingEnv) Step(action any) (Step, error) {
	got, err := e.local.Call("env_step", map[string]any{"env": e.ID, "action": action})
	if err != nil {
		return Step{}, err
	}
	return Step{got["obs"].(map[string]any), got["reward"].(float64), got["terminated"].(bool), got["truncated"].(bool), got["info"].(map[string]any)}, nil
}

// Close lets the environment go.
func (e *TradingEnv) Close() { _, _ = e.local.Call("env_close", map[string]any{"env": e.ID}) }
