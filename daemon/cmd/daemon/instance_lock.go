package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type daemonInstanceLock struct {
	file     *os.File
	unlock   func() error
	once     sync.Once
	closeErr error
}

func acquireDaemonInstanceLock(path string) (*daemonInstanceLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create instance lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure instance lock permissions: %w", err)
	}
	unlock, err := lockFileExclusive(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another daemon already owns %s: %w", path, err)
	}
	return &daemonInstanceLock{file: file, unlock: unlock}, nil
}

func (l *daemonInstanceLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.unlock != nil {
			l.closeErr = l.unlock()
		}
		if l.file != nil {
			if err := l.file.Close(); l.closeErr == nil {
				l.closeErr = err
			}
		}
	})
	return l.closeErr
}
