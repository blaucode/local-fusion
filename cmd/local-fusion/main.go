// Command local-fusion is the v2 server binary: a multi-model quality gate for
// coding agents, exposed over MCP (Streamable HTTP + stdio).
//
// Status: M0 scaffold. `serve` lands in M2 (see product-docs/PROJECT-PLAN.md).
package main

import (
	"fmt"
	"os"

	"local-fusion/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version.String())
	case "serve":
		fmt.Fprintln(os.Stderr, "local-fusion serve: not implemented yet — ships in M2 (product-docs/PROJECT-PLAN.md)")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: local-fusion <version|serve>")
}
