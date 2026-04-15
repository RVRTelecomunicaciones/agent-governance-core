package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("agent-governance-core starting...")

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// TODO: wire dependencies, start HTTP server
	return nil
}
