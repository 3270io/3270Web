package assets

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:generate go-bindata -pkg assets -o bindata.go -prefix ../../s3270-bin ../../s3270-bin/s3270.exe

// ExtractS3270 writes the embedded s3270 binary to a temp location and returns the path.
func ExtractS3270() (string, error) {
	name := s3270BinaryName()
	data, err := Asset(name)
	if err != nil {
		return "", fmt.Errorf("embedded binary not found: %w", err)
	}

	cacheDir := filepath.Join(os.TempDir(), "3270Web")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	outPath := filepath.Join(cacheDir, name)
	if info, err := os.Stat(outPath); err == nil && info.Size() == int64(len(data)) {
		return outPath, nil
	}

	if err := os.WriteFile(outPath, data, 0o755); err != nil {
		return "", err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(outPath, 0o755); err != nil {
			return "", err
		}
	}

	return outPath, nil
}

func s3270BinaryName() string {
	if runtime.GOOS == "windows" {
		return "s3270.exe"
	}
	return "s3270"
}
