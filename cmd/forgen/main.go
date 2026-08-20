// Command forgen es un agente de código agnóstico a lenguaje y proveedor.
package main

import (
	"fmt"
	"os"

	"github.com/forgen/forgen/internal/adapters/in/cli"
)

func main() {
	root, err := cli.NewRootCommand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "forgen: %v\n", err)
		os.Exit(1)
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "forgen: %v\n", err)
		os.Exit(1)
	}
}
