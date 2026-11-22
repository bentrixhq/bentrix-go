//go:build windows

package mcp

import "os"

// alive answers whether a process is running: on Windows FindProcess opens it, and fails for one that
// has ended.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
