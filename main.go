// Command ship manages apps on one personal VPS.
package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/harrisonclewis/ship/internal/cli"
)

//go:embed ship-agent
var agentScript []byte

func main() {
	err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, agentScript)
	if err == nil {
		return
	}
	if cli.IsUsage(err) {
		fmt.Fprintln(os.Stderr, "ship:", err)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "ship:", err)
	os.Exit(1)
}
