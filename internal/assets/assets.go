package assets

import (
	"fmt"
	"os"
	"path/filepath"
)

//go:generate go-bindata -pkg assets -o bindata.go -prefix ../../s3270-bin ../../s3270-bin/s3270.exe

// ExtractS3270 writes the embedded s3270 binary to a temp location and returns the path.
func ExtractS3270() (string, error) {
	data, err := Asset("s3270.exe")
	if err != nil {
		return "", fmt.Errorf("embedded binary not found: %w", err)
	}

	cacheDir := filepath.Join(os.TempDir(), "h3270")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	outPath := filepath.Join(cacheDir, "s3270.exe")
	if info, err := os.Stat(outPath); err == nil && info.Size() == int64(len(data)) {
		return outPath, nil
	}

	if err := os.WriteFile(outPath, data, 0o755); err != nil {
		return "", err
	}

	return outPath, nil
}
