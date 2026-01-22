//go:build !windows
// +build !windows

package main

import (
	"fmt"
	"os"
)

func showFatalError(message string) {
	fmt.Fprintf(os.Stderr, "3270Web fatal error: %s\n", message)
}
