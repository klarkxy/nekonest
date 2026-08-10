//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import "syscall"

func setPrivateUmask() {
	syscall.Umask(0o077)
}
