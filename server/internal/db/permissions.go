package db

import (
	"fmt"
	"os"
)

const privateDatabaseMode os.FileMode = 0o600

func preparePrivateDatabase(dbPath string) error {
	file, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, privateDatabaseMode)
	if err != nil {
		return fmt.Errorf("open private sqlite database: %w", err)
	}
	if err := file.Chmod(privateDatabaseMode); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure sqlite database: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close private sqlite database: %w", err)
	}
	return tightenPrivateDatabaseArtifacts(dbPath)
}

func tightenPrivateDatabaseArtifacts(dbPath string) error {
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Chmod(path, privateDatabaseMode); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("secure sqlite artifact: %w", err)
		}
	}
	return nil
}
