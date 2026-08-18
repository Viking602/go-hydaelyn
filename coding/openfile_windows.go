//go:build windows

package coding

import (
	"os"
	"syscall"
)

func openRegularRead(path string) (*os.File, error) {
	return openWindowsLeaf(path, syscall.GENERIC_READ)
}

func openRegularWrite(path string, _ os.FileMode) (*os.File, error) {
	f, err := openWindowsLeaf(path, syscall.GENERIC_WRITE)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func openWindowsLeaf(path string, access uint32) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(
		name,
		access,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		_ = syscall.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = syscall.CloseHandle(handle)
		return nil, ErrNotRegularFile
	}
	return os.NewFile(uintptr(handle), path), nil
}
