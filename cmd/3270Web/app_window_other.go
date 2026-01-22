//go:build !windows

package main

import (
	"fmt"
	"log"
	"os"
)

func showFatalError(message string) {
	log.Printf("Fatal error: %s", message)
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func runAppWindow(url string, shutdown func()) {}
