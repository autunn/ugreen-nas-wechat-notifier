package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

func ensureRuntimeDirs() {
	dataDir := strings.TrimSpace(os.Getenv("UGAPP_DATA_DIR"))
	if dataDir == "" {
		dataDir = "data"
	}
	logDir := strings.TrimSpace(os.Getenv("UGAPP_LOG_DIR"))
	if logDir == "" {
		logDir = filepath.Join(dataDir, "log")
	}

	for _, dir := range []string{
		filepath.Join(dataDir, "config"),
		logDir,
		filepath.Join(dataDir, "token"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Fatalf("create runtime dir failed (%s): %v", dir, err)
		}
	}
}
