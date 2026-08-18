//go:build !unix

package coding

import "os"

const (
	readOpenFlags   = os.O_RDONLY
	writeOpenFlags  = os.O_WRONLY | os.O_TRUNC
	createOpenFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
)
