package main

import (
	"log"
	"os"
)

func showFatalError(message string) {
	log.Printf("Fatal error: %s", message)
	os.Exit(1)
}
