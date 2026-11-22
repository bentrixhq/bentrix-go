package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A passing refusal does not end a stream, and a rewind is said (the strict audit of 2026-11-04,
// S3/S4): over a stub that answers busy, an item, expired, a rewind and then an item that must not be
// handed, Watch hands the first item and ends with *Rewound at the rewind's cursor.
//
// Mutations (2026-11-04), each red: the retry removed (the busy ended the stream); the
// rewind not said (the item after it was handed).
func TestAStreamAsksAgainWhenBusyAndEndsOnARewindWithItsCursor(t *testing.T) {
	pages := []map[string]any{
		{"err": "busy"},
		{"items": []any{1}, "next": "c1"},
		{"err": "expired"},
		{"items": []any{}, "next": "c0", "rewound": true},
		{"items": []any{2}, "next": "c2"},
	}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID any `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		page := map[string]any{"items": []any{}, "next": "cz"}
		if len(pages) > 0 {
			page, pages = pages[0], pages[1:]
		}
		mu.Unlock()
		var result map[string]any
		if m, ok := page["err"].(string); ok {
			result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": m}}, "_meta": map[string]any{"bentrix/machine": m}}
		} else {
			result = map[string]any{"structuredContent": page}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()
	c := newClient(srv.URL+"/mcp", "tok", "1.21.0")
	var got []any
	var ended error
	for item, err := range c.Watch(context.Background(), "bentrix://x", nil, WatchOptions{}) {
		if err != nil {
			ended = err
			break
		}
		got = append(got, item)
		if len(got) >= 2 {
			break
		}
	}
	if len(got) != 1 {
		t.Fatalf("the items handed: %v (ended %v)", got, ended)
	}
	var rw *Rewound
	if !errors.As(ended, &rw) || rw.Cursor != "c0" {
		t.Fatalf("the rewind was not said with its cursor: %v", ended)
	}
}
