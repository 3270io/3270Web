// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows
// +build !windows

package main

// runAppWindow is a no-op on non-Windows platforms.
func runAppWindow(url string, onExit func()) {
	if onExit != nil {
		onExit()
	}
}
