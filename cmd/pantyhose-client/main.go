package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "pantyhose-client %s\n", version)
	fmt.Fprintln(os.Stderr, "Not yet implemented. See TODO.md for roadmap.")
	os.Exit(1)
}
