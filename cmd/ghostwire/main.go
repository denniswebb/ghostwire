package main

import (
	"fmt"
	"os"

	"github.com/denniswebb/ghostwire/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "ghostwire: %v\n", err)
		os.Exit(1)
	}
}
