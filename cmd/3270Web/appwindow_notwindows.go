//go:build !windows
// +build !windows

package main

// runAppWindow is a no-op on non-Windows platforms.
func runAppWindow(url string, onExit func()) {
	if onExit != nil {
		onExit()
	}
}
