// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package main

import (
	"fmt"
	"os"
)

func showFatalError(message string) {
	fmt.Fprintf(os.Stderr, "3270Web fatal error: %s\n", message)
}
