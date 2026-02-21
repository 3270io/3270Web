package assets

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:generate go-bindata -pkg assets -o bindata.go -prefix ../../s3270-bin ../../s3270-bin/...

// ExtractS3270 writes the embedded s3270 binary for the current platform to a temp location.
func ExtractS3270() (string, error) {
	assetName, err := findEmbeddedS3270AssetName(runtime.GOOS, runtime.GOARCH, AssetNames())
	if err != nil {
		return "", err
	}

	data, err := Asset(assetName)
	if err != nil {
		return "", fmt.Errorf("embedded binary %q could not be loaded: %w", assetName, err)
	}

	cacheDir := filepath.Join(os.TempDir(), "3270Web")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	outPath := filepath.Join(cacheDir, assetName)
	if info, err := os.Stat(outPath); err == nil && info.Size() == int64(len(data)) {
		return outPath, nil
	}

	if err := os.WriteFile(outPath, data, 0o755); err != nil {
		return "", err
	}

	return outPath, nil
}

func findEmbeddedS3270AssetName(goos, goarch string, available []string) (string, error) {
	assets := make(map[string]struct{}, len(available))
	for _, name := range available {
		assets[name] = struct{}{}
	}
	for _, candidate := range embeddedS3270AssetCandidates(goos, goarch) {
		if _, ok := assets[candidate]; ok {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("embedded s3270 binary not found for %s/%s", goos, goarch)
}

func embeddedS3270AssetCandidates(goos, goarch string) []string {
	switch goos {
	case "windows":
		return []string{
			"s3270-windows-" + goarch + ".exe",
			"s3270-windows.exe",
			"s3270.exe",
		}
	default:
		return []string{
			"s3270-" + goos + "-" + goarch,
			"s3270-" + goos,
			"s3270",
		}
	}
}
