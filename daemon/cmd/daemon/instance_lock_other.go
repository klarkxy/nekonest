//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !solaris

package main

import (
	"fmt"
	"os"
)

func lockFileExclusive(*os.File) (func() error, error) {
	return nil, fmt.Errorf("reliable process locking is unsupported on this platform")
}
