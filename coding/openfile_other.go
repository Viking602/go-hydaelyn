//go:build !unix && !windows

package coding

import "os"

func openRegularRead(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY, 0)
}

func openRegularWrite(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, perm)
}
