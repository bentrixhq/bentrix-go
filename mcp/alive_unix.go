//go:build !windows

package mcp

import (
	"errors"
	"os"
	"syscall"
)

// alive answers whether a process is running: signal 0 reaches it, or is refused only for permission.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
