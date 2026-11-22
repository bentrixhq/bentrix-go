// Package bentrix is BENTRIX's engine in your own Go process, through its C ABI
// (crates/bentrix-ffi/include/bentrix.h), and its Gym - a client of BENTRIX's own environment
// (env_make, env_reset, env_step), the one every language reaches.
//
// Build with the library's folder on the linker's path: CGO_LDFLAGS="-L<dir>" and, at run time,
// LD_LIBRARY_PATH (or the system's equivalent). A Local is used from one goroutine at a time.
package bentrix

/*
#cgo CFLAGS: -I${SRCDIR}/../include -I${SRCDIR}/../../../crates/bentrix-ffi/include
#cgo LDFLAGS: -lbentrix
#include <stdlib.h>
#include "bentrix.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"strings"
	"unsafe"
)

// Error is a refusal or a failure, in BENTRIX's own words.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func lastError() error { return &Error{C.GoString(C.bx_last_error())} }

func cstr(s string) *C.char { return C.CString(s) }

func take(p *C.char) (string, error) {
	if p == nil {
		return "", lastError()
	}
	defer C.bx_free(p)
	return C.GoString(p), nil
}

// Local is an embedded BENTRIX over a workspace and its recordings.
type Local struct{ s *C.BxSession }

// Open opens workspace (yours when "") and its recordings (yours when ""). A window or a --serve
// already on the workspace refuses it by name.
func Open(workspace, store string) (*Local, error) {
	var ws, st *C.char
	if workspace != "" {
		ws = cstr(workspace)
		defer C.free(unsafe.Pointer(ws))
	}
	if store != "" {
		st = cstr(store)
		defer C.free(unsafe.Pointer(st))
	}
	s := C.bx_open(ws, st)
	if s == nil {
		return nil, lastError()
	}
	return &Local{s}, nil
}

// Close persists the workspace and lets it go.
func (l *Local) Close() {
	if l.s != nil {
		C.bx_close(l.s)
		l.s = nil
	}
}

// Call calls one tool by name with args (any JSON-able value; nil for none) and decodes its answer.
func (l *Local) Call(tool string, args any) (map[string]any, error) {
	var a *C.char
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		a = cstr(string(raw))
		defer C.free(unsafe.Pointer(a))
	}
	t := cstr(tool)
	defer C.free(unsafe.Pointer(t))
	text, err := take(C.bx_call(l.s, t, a))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal([]byte(text), &out)
}

// Read reads one resource by its catalogue name, its template's variables.
func (l *Local) Read(resource string, vars map[string]string) (map[string]any, error) {
	raw, _ := json.Marshal(vars)
	r, v := cstr(resource), cstr(string(raw))
	defer C.free(unsafe.Pointer(r))
	defer C.free(unsafe.Pointer(v))
	text, err := take(C.bx_read(l.s, r, v))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal([]byte(text), &out)
}

// Arm arms a workbench's engine (its venue over its recording).
func (l *Local) Arm(bench string) error {
	b := cstr(bench)
	defer C.free(unsafe.Pointer(b))
	if C.bx_arm(l.s, b, 20000) != 0 {
		return lastError()
	}
	return nil
}

// Advance advances a workbench to toMs on its clock, as Play would; where it stands.
func (l *Local) Advance(bench string, toMs int64) (int64, error) {
	b := cstr(bench)
	defer C.free(unsafe.Pointer(b))
	var reached C.int64_t
	if C.bx_advance(l.s, b, C.int64_t(toMs), &reached) != 0 {
		return 0, lastError()
	}
	return int64(reached), nil
}

// Book reads a workbench's open positions and pending orders.
func (l *Local) Book(bench string) (map[string]any, error) {
	p := strings.Split(strings.TrimPrefix(bench, "bentrix://"), "/")
	if len(p) < 4 {
		return nil, errors.New("not a workbench: " + bench)
	}
	return l.Read("trade_book", map[string]string{"home": p[0], "owner": p[1], "bench": p[3]})
}

// Version is the build of the engine loaded.
func Version() string { return C.GoString(C.bx_version()) }

// IntervalMs is an interval's length in ms (M1, H4, D1); 0 for one with no fixed length.
func IntervalMs(label string) int64 {
	l := cstr(label)
	defer C.free(unsafe.Pointer(l))
	return int64(C.bx_interval_ms(l))
}

// MakeTape writes the CI tape into directory and answers its workspace and store.
func MakeTape(directory string) (string, string, error) {
	d := cstr(directory)
	defer C.free(unsafe.Pointer(d))
	var ws, st *C.char
	if C.bx_make_tape(d, &ws, &st) != 0 {
		return "", "", lastError()
	}
	w, _ := take(ws)
	s, _ := take(st)
	return w, s, nil
}
