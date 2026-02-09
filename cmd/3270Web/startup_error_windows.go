//go:build windows

package main

import "github.com/lxn/walk"

func showFatalError(message string) {
	_ = walk.MsgBox(nil, "3270Web", message, walk.MsgBoxIconError)
}
