package main

import "os"

const (
	privateDirectoryMode os.FileMode = 0o700
	privateFileMode      os.FileMode = 0o600
)

func preparePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, privateDirectoryMode); err != nil {
		return err
	}
	return os.Chmod(path, privateDirectoryMode)
}

func writePrivateFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, privateFileMode); err != nil {
		return err
	}
	return os.Chmod(path, privateFileMode)
}
