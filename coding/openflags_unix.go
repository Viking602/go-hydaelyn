//go:build unix

package coding

import (
	"os"
	"syscall"
)

const (
	readOpenFlags   = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	writeOpenFlags  = os.O_WRONLY | os.O_TRUNC | syscall.O_NOFOLLOW
	createOpenFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
)
