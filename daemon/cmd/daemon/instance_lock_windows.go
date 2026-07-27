//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockFileExclusive(file *os.File) (func() error, error) {
	var overlapped windows.Overlapped
	handle := windows.Handle(file.Fd())
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, &overlapped); err != nil {
		return nil, err
	}
	return func() error {
		return windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
	}, nil
}
