package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jnnngs/3270Web/internal/assets"
	"github.com/jnnngs/3270Web/internal/config"
)

func resolveS3270Path(execPath string) string {
	if runtime.GOOS == "windows" {
		if execPath != "" {
			candidate := execPath
			if info, err := os.Stat(candidate); err == nil {
				if info.IsDir() {
					candidate = filepath.Join(candidate, s3270BinaryName())
				}
				if fileExists(candidate) {
					return candidate
				}
			}
		}

		local := filepath.Join(".", "s3270-bin", s3270BinaryName())
		if fileExists(local) {
			return local
		}

		// Embedded binary is Windows-only (s3270.exe); other platforms must use system s3270.
		if embedded, err := assets.ExtractS3270(); err == nil {
			return embedded
		}
	}

	if execPath != "" && execPath != "/usr/local/bin" {
		return filepath.Join(execPath, s3270BinaryName())
	}

	local := filepath.Join(".", "s3270-bin", s3270BinaryName())
	if fileExists(local) {
		return local
	}

	if path, err := exec.LookPath(s3270BinaryName()); err == nil {
		return path
	}

	return filepath.Join("/usr/local/bin", "s3270")
}

func s3270BinaryName() string {
	if runtime.GOOS == "windows" {
		return "s3270.exe"
	}
	return "s3270"
}

func buildS3270Args(opts config.S3270Options, hostname string) []string {
	envOverrides, err := config.S3270EnvOverridesFromEnv()
	if err != nil {
		log.Printf("Warning: invalid .env s3270 options: %v", err)
	}

	model := opts.Model
	if envOverrides.HasModel {
		model = envOverrides.Model
	}
	args := []string{}
	if model != "" {
		args = append(args, "-model", model)
	}

	if envOverrides.HasCodePage {
		if envOverrides.CodePage != "" {
			args = append(args, "-codepage", envOverrides.CodePage)
		}
	} else if opts.Charset != "" && opts.Charset != "bracket" {
		args = append(args, "-charset", opts.Charset)
	}

	args = append(args, envOverrides.Args...)
	if opts.Additional != "" {
		if additional, err := config.SplitArgs(opts.Additional); err == nil {
			args = append(args, additional...)
		} else {
			log.Printf("Warning: invalid s3270-options additional arguments: %v", err)
		}
	}
	if len(envOverrides.ExecArgs) > 0 {
		args = append(args, envOverrides.ExecArgs...)
		return args
	}
	if strings.TrimSpace(hostname) != "" {
		args = append(args, hostname)
	}
	return args
}
