//go:build !windows

package main

// runAppWindow keeps the server running in console mode on non-Windows platforms.
func runAppWindow(url string, shutdown func()) {
	// No-op on non-Windows platforms; server runs in console.
}
