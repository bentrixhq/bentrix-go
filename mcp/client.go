// Package mcp is BENTRIX's MCP client for Go: this file by hand - the connection and everything that does
// not change when a capability is added - and generated.go from the capability catalogue. It speaks MCP
// revision 2026-07-28 over Streamable HTTP with net/http: every request is its own POST carrying the
// protocol version, the client's identity and its (empty) capabilities in _meta - the same wire as the
// Python client's _runtime.py and the JavaScript client's client.js, field for field.
//
// Connect finds the running application through endpoint.json in BENTRIX's agents folder, pairs with it
// through local.secret under a name of this process, and connects with the token it gets.
package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// The wire's constants.
const (
	ProtocolVersion = "2026-07-28"
	ClientName      = "bentrix-go-sdk"
	ClientVersion   = "0.1.0"
	EndpointFile    = "endpoint.json"
	SecretFile      = "local.secret"
)

// Error is a refusal or a failure, in BENTRIX's own words.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// RPCError is a JSON-RPC error: the request itself was refused.
type RPCError struct {
	Code int64
	Msg  string
	// RetryAfterMs is the wait a busy answer names (Busy), 0 when it names none.
	RetryAfterMs int64
}

// Busy is the JSON-RPC code Bentrix answers when it refused a request BEFORE doing anything with it:
// one program's 20 calls a second spent, too many connections or streams, a task it could not read
// in time. Nothing ran, so the client sends the same request again after the wait the answer names
// (retryAfterMs, a second when none), at most BusyRetries times and each wait at most BusyWaitMax;
// then the refusal is returned. An order is never sent twice by this.
const (
	Busy        int64 = -40001
	BusyRetries       = 10
	BusyWaitMax       = 5 * time.Second
)

// busyWait is the wait a busy answer names, bounded.
func busyWait(e *RPCError) time.Duration {
	w := time.Second
	if e.RetryAfterMs > 0 {
		w = time.Duration(e.RetryAfterMs) * time.Millisecond
	}
	if w > BusyWaitMax {
		w = BusyWaitMax
	}
	return w
}

func (e *RPCError) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Msg) }

// ToolError is a tool that ran and answered isError: the product's own sentence, verbatim. Machine is
// the server's word for the kind (busy, expired, insufficient_scope...) when it gave one.
type ToolError struct {
	Tool    string
	Msg     string
	Machine string
}

func (e *ToolError) Error() string { return e.Tool + ": " + e.Msg }

// Rewound ends a Watch whose clock went back behind its cursor (a laboratory's rewind): what came after
// it will come again, so it is not handed twice as if new. Cursor is where to read on from (After).
type Rewound struct {
	URI    string
	Cursor string
}

func (e *Rewound) Error() string {
	return e.URI + ": the workbench's clock went back; read on from " + e.Cursor
}

// A stream's call answered with one of these is asked again, after a pause.
var passing = map[string]bool{"busy": true, "expired": true}

const (
	passingTries = 5
	passingPause = 500 * time.Millisecond
)

// The discovery and pairing failures.
var (
	ErrNotRunning            = errors.New("Bentrix is not running")
	ErrPairingRefused        = errors.New("Bentrix refused the pairing")
	ErrIncompatibleCatalogue = errors.New("the server's catalogue is on another major line")
	ErrJobFailed             = errors.New("the job failed")
)

// Ptr is a pointer to v: an optional argument given (mcp.Ptr(0.01)).
func Ptr[T any](v T) *T { return &v }

// WatchOptions are an event stream's: After, the cursor to read since (empty: only what is new), and
// Timeout, how long one wait lasts before it asks again (zero: 30 s).
type WatchOptions struct {
	After   string
	Timeout time.Duration
}

// LongPollMargin is how much longer than it asks the server to wait a call is given (found 2026-11-02:
// wait_for beyond the client's own timeout ended in the client before the server could answer).
const LongPollMargin = 15 * time.Second

// Client is one BENTRIX endpoint. Embedded in the generated Bentrix.
type Client struct {
	URL              string
	CatalogueVersion string
	token            string
	http             *http.Client
	timeout          time.Duration
	id               atomic.Int64
}

func newClient(url, token, catalogueVersion string) *Client {
	return &Client{URL: url, CatalogueVersion: catalogueVersion, token: token, http: &http.Client{}, timeout: 60 * time.Second}
}

// plain is the arguments as a JSON object: a struct through its json tags (an absent optional left out
// by omitempty), a map with its nil values left out, nil as {}. Numbers stay as written (json.Number),
// so an int64 past 2^53 is not rounded through a float64.
func plain(args any) (map[string]any, error) {
	out := map[string]any{}
	if args == nil {
		return out, nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	for k, v := range out {
		if v == nil {
			delete(out, k)
		}
	}
	return out, nil
}

func headerValue(v string) string {
	safe := v == strings.TrimSpace(v) && !(strings.HasPrefix(v, "=?base64?") && strings.HasSuffix(v, "?="))
	for _, r := range v {
		if r < 0x20 || r > 0x7e {
			safe = false
		}
	}
	if safe {
		return v
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(v)) + "?="
}

func scalar(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

var templatePart = regexp.MustCompile(`\{([?&]?)([^}]+)\}`)

// expandTemplate is the RFC 6570 subset the catalogue uses: {name} and {?a,b}. An absent optional is
// left out.
func expandTemplate(template string, vars any) (string, error) {
	values, err := plain(vars)
	if err != nil {
		return "", err
	}
	var missing string
	out := templatePart.ReplaceAllStringFunc(template, func(m string) string {
		g := templatePart.FindStringSubmatch(m)
		op, names := g[1], g[2]
		if op != "" {
			var pairs []string
			for _, n := range strings.Split(names, ",") {
				if v, ok := values[n]; ok {
					pairs = append(pairs, n+"="+url.QueryEscape(scalar(v)))
				}
			}
			if len(pairs) == 0 {
				return ""
			}
			return op + strings.Join(pairs, "&")
		}
		v, ok := values[names]
		if !ok {
			missing = names
			return ""
		}
		return url.PathEscape(scalar(v))
	})
	if missing != "" {
		return "", fmt.Errorf("the URI template %s needs %s", template, missing)
	}
	return out, nil
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int64           `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// rpc is one request, sent again while Bentrix answers busy (Busy): it ran nothing.
func (c *Client) rpc(ctx context.Context, method string, params map[string]any, name string, timeout time.Duration) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		out, err := c.rpcOnce(ctx, method, params, name, timeout)
		var busy *RPCError
		if err == nil || !errors.As(err, &busy) || busy.Code != Busy || attempt == BusyRetries {
			return out, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(busyWait(busy)):
		}
	}
}

func (c *Client) rpcOnce(ctx context.Context, method string, params map[string]any, name string, timeout time.Duration) (json.RawMessage, error) {
	if timeout < c.timeout {
		timeout = c.timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	id := c.id.Add(1)
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": ClientName, "version": ClientVersion},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if name != "" {
		req.Header.Set("Mcp-Name", headerValue(name))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	var msg rpcMessage
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		m, err := readSSE(buf.String(), id)
		if err != nil {
			return nil, err
		}
		msg = m
	} else if err := json.Unmarshal(buf.Bytes(), &msg); err != nil {
		return nil, &RPCError{Code: int64(resp.StatusCode), Msg: fmt.Sprintf("HTTP %d with no JSON-RPC body", resp.StatusCode)}
	}
	if msg.Error != nil {
		var data struct {
			RetryAfterMs int64 `json:"retryAfterMs"`
		}
		_ = json.Unmarshal(msg.Error.Data, &data) // any other shape of data names no wait
		return nil, &RPCError{Code: msg.Error.Code, Msg: msg.Error.Message, RetryAfterMs: data.RetryAfterMs}
	}
	return msg.Result, nil
}

// readSSE is the final response on a request-scoped event stream; notifications before it are ignored.
func readSSE(text string, id int64) (rpcMessage, error) {
	var data []string
	for _, line := range append(strings.Split(text, "\n"), "") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			if len(data) > 0 {
				var m rpcMessage
				if json.Unmarshal([]byte(strings.Join(data, "\n")), &m) == nil && string(m.ID) == strconv.FormatInt(id, 10) && (m.Result != nil || m.Error != nil) {
					return m, nil
				}
			}
			data = nil
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimLeft(line[5:], " "))
		}
	}
	return rpcMessage{}, &Error{"the stream ended without a response"}
}

// decode reads JSON into an any: objects map[string]any, arrays []any, numbers float64 (strconv reads
// the double the engine wrote).
func decode(raw []byte) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// Call is a tool by its wire name, args a struct, a map or nil.
func (c *Client) Call(ctx context.Context, name string, args any) (any, error) {
	a, err := plain(args)
	if err != nil {
		return nil, err
	}
	var wait time.Duration
	if ms, ok := a["timeout_ms"].(json.Number); ok {
		if f, err := ms.Float64(); err == nil {
			wait = time.Duration(f*float64(time.Millisecond)) + LongPollMargin
		}
	}
	raw, err := c.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": a}, name, wait)
	if err != nil {
		return nil, err
	}
	var res struct {
		ResultType        string          `json:"resultType"`
		IsError           bool            `json:"isError"`
		Meta              struct {
			Machine string `json:"bentrix/machine"`
		} `json:"_meta"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if res.ResultType == "input_required" {
		return nil, &Error{name + " needs input this client cannot give; answer it in Bentrix's window"}
	}
	var text strings.Builder
	for _, part := range res.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if res.IsError {
		msg := text.String()
		if msg == "" {
			msg = "the tool failed"
		}
		return nil, &ToolError{Tool: name, Msg: msg, Machine: res.Meta.Machine}
	}
	if len(res.StructuredContent) > 0 && string(res.StructuredContent) != "null" {
		return decode(res.StructuredContent)
	}
	if text.Len() > 0 {
		if v, err := decode([]byte(text.String())); err == nil {
			return v, nil
		}
	}
	return map[string]any{}, nil
}

// Job is a long operation: Wait answers its result once it completes (the person may act first).
type Job struct {
	c      *Client
	Handle string
	result any
}

// CallJob is a long operation: a Job whether the server answered at once or with a handle.
func (c *Client) CallJob(ctx context.Context, name string, args any) (*Job, error) {
	raw, err := c.Call(ctx, name, args)
	if err != nil {
		return nil, err
	}
	if m, ok := raw.(map[string]any); ok && len(m) == 1 {
		if h, ok := m["job"].(string); ok {
			return &Job{c: c, Handle: h}, nil
		}
	}
	return &Job{c: c, result: raw}, nil
}

// Status is the job's state.
func (j *Job) Status(ctx context.Context) (map[string]any, error) {
	if j.Handle == "" {
		return map[string]any{"status": "completed"}, nil
	}
	v, err := j.c.Call(ctx, "job_get", map[string]any{"job": j.Handle})
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	return m, nil
}

// Cancel asks the job to stop.
func (j *Job) Cancel(ctx context.Context) error {
	if j.Handle == "" {
		return nil
	}
	_, err := j.c.Call(ctx, "job_cancel", map[string]any{"job": j.Handle})
	return err
}

// Wait polls until the job completes, fails or ctx ends.
func (j *Job) Wait(ctx context.Context, poll time.Duration) (any, error) {
	if j.Handle == "" {
		return j.result, nil
	}
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	for {
		st, err := j.Status(ctx)
		if err != nil {
			return nil, err
		}
		switch st["status"] {
		case "completed":
			if r, ok := st["result"]; ok && r != nil {
				return r, nil
			}
			return map[string]any{}, nil
		case "failed", "cancelled":
			return nil, fmt.Errorf("%w: job %s %v: %v", ErrJobFailed, j.Handle, st["status"], st["message"])
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}

func (c *Client) readURI(ctx context.Context, uri string) (any, error) {
	raw, err := c.rpc(ctx, "resources/read", map[string]any{"uri": uri}, uri, 0)
	if err != nil {
		return nil, err
	}
	var res struct {
		Contents []struct {
			Text *string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if len(res.Contents) == 0 || res.Contents[0].Text == nil {
		return nil, &Error{uri + " answered no text content"}
	}
	return decode([]byte(*res.Contents[0].Text))
}

// ReadResource reads a resource by its URI template and variables.
func (c *Client) ReadResource(ctx context.Context, template string, vars any) (any, error) {
	uri, err := expandTemplate(template, vars)
	if err != nil {
		return nil, err
	}
	return c.readURI(ctx, uri)
}

// ReadPages yields every page of a cursor-paged resource: next is the URI of the following page.
func (c *Client) ReadPages(ctx context.Context, template string, vars any) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		uri, err := expandTemplate(template, vars)
		for uri != "" || err != nil {
			if err != nil {
				yield(nil, err)
				return
			}
			var page any
			page, err = c.readURI(ctx, uri)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(page, nil) {
				return
			}
			uri = ""
			if m, ok := page.(map[string]any); ok {
				uri, _ = m["next"].(string)
			}
		}
	}
}

// Watch yields the items of a stream, forever: wait_for long-polls, a page is {items, next}. A call
// answered busy or expired is asked again (five times, half a second apart); a page saying rewound ends
// the stream with *Rewound, carrying the cursor to read on from (the strict audit of 2026-11-04, S3/S4).
func (c *Client) Watch(ctx context.Context, template string, vars any, opts WatchOptions) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		uri, err := expandTemplate(template, vars)
		if err != nil {
			yield(nil, err)
			return
		}
		timeout := opts.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		cursor := opts.After
		for {
			args := map[string]any{"uri": uri, "timeout_ms": timeout.Milliseconds()}
			if cursor != "" {
				args["after"] = cursor
			}
			var v any
			var err error
			for attempt := 0; ; attempt++ {
				v, err = c.Call(ctx, "wait_for", args)
				var te *ToolError
				if err == nil || !errors.As(err, &te) || !passing[te.Machine] || attempt >= passingTries-1 {
					break
				}
				select {
				case <-ctx.Done():
					yield(nil, ctx.Err())
					return
				case <-time.After(passingPause):
				}
			}
			if err != nil {
				yield(nil, err)
				return
			}
			page, _ := v.(map[string]any)
			items, _ := page["items"].([]any)
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
			if next, ok := page["next"].(string); ok {
				cursor = next
			}
			if rewound, _ := page["rewound"].(bool); rewound {
				yield(nil, &Rewound{URI: uri, Cursor: cursor})
				return
			}
		}
	}
}

func major(v string) (int, error) {
	p := strings.Split(v, ".")
	if len(p) != 3 {
		return 0, fmt.Errorf("%w: %s is not a version major.minor.patch", ErrIncompatibleCatalogue, v)
	}
	n, err := strconv.Atoi(p[0])
	if err != nil {
		return 0, fmt.Errorf("%w: %s is not a version major.minor.patch", ErrIncompatibleCatalogue, v)
	}
	return n, nil
}

// VerifyCatalogue refuses a server on another major line of the catalogue.
func (c *Client) VerifyCatalogue(ctx context.Context) (string, error) {
	me, err := c.Call(ctx, "agent_whoami", nil)
	if err != nil {
		return "", err
	}
	server, _ := me.(map[string]any)["catalogue_version"].(string)
	a, err := major(server)
	if err != nil {
		return "", err
	}
	b, err := major(c.CatalogueVersion)
	if err != nil {
		return "", err
	}
	if a != b {
		return "", fmt.Errorf("%w: this SDK was generated from catalogue %s; the server's is %s", ErrIncompatibleCatalogue, c.CatalogueVersion, server)
	}
	return server, nil
}

// AgentsDir is the folder BENTRIX writes its endpoint and secret into - user_dir(Data, "agents"), as the
// application has it.
func AgentsDir() string {
	if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
		return filepath.Join(lad, "BENTRIX", "agents")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".local", "share", "bentrix", "agents")
	}
	return "bentrix-agents"
}

// ReadEndpoint is the running application's URL, from its endpoint file.
func ReadEndpoint(folder string) (string, error) {
	file := filepath.Join(folder, EndpointFile)
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("%w: there is no readable %s in %s", ErrNotRunning, EndpointFile, folder)
	}
	var doc struct {
		URL string `json:"url"`
		PID *int   `json:"pid"`
	}
	if json.Unmarshal(raw, &doc) != nil || !strings.HasPrefix(doc.URL, "http://127.0.0.1:") || doc.PID == nil {
		return "", fmt.Errorf("%w: %s is not an endpoint file", ErrNotRunning, file)
	}
	if !alive(*doc.PID) {
		return "", fmt.Errorf("%w: %s is stale (process %d has ended)", ErrNotRunning, file, *doc.PID)
	}
	return doc.URL, nil
}

// ReadSecret is the secret BENTRIX wrote at its start.
func ReadSecret(folder string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(folder, SecretFile))
	if err != nil {
		return "", fmt.Errorf("%w: there is no readable %s in %s: Bentrix writes it at every start", ErrPairingRefused, SecretFile, folder)
	}
	secret := strings.TrimSpace(string(raw))
	if secret == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrPairingRefused, SecretFile)
	}
	return secret, nil
}

// DefaultName is the name this process pairs under: BENTRIX_AGENT_NAME, else the program's name.
func DefaultName() string {
	if n := strings.TrimSpace(os.Getenv("BENTRIX_AGENT_NAME")); n != "" {
		return n
	}
	if len(os.Args) > 0 && os.Args[0] != "" {
		return ClientName + " (" + filepath.Base(os.Args[0]) + ")"
	}
	return ClientName
}

// Pair presents the secret to url/pair and answers the token. The secret travels in this one body.
func Pair(ctx context.Context, url, secret, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{"secret": secret, "client": map[string]any{"name": name}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/")+"/pair", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var doc struct {
		Token string `json:"token"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&doc)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if doc.Error.Code == "wrong_secret" {
			return "", fmt.Errorf("%w: Bentrix refused the secret in the secret file: it writes a new secret at every start", ErrPairingRefused)
		}
		return "", fmt.Errorf("%w: HTTP %d %s: %s", ErrPairingRefused, resp.StatusCode, doc.Error.Code, doc.Error.Message)
	}
	if doc.Token == "" {
		return "", fmt.Errorf("%w: Bentrix answered the pairing request with no token", ErrPairingRefused)
	}
	return doc.Token, nil
}

// Discover finds the running application and gets a token. agentsFolder and name may be empty.
func Discover(ctx context.Context, agentsFolder, name string) (url, token string, err error) {
	if agentsFolder == "" {
		agentsFolder = AgentsDir()
	}
	if name == "" {
		name = DefaultName()
	}
	if url, err = ReadEndpoint(agentsFolder); err != nil {
		return "", "", err
	}
	secret, err := ReadSecret(agentsFolder)
	if err != nil {
		return "", "", err
	}
	token, err = Pair(ctx, url, secret, name)
	return url, token, err
}

// Connect finds the running application, pairs under name (empty: DefaultName) and connects.
func Connect(ctx context.Context, name string) (*Bentrix, error) {
	url, token, err := Discover(ctx, "", name)
	if err != nil {
		return nil, err
	}
	return New(url, token), nil
}
