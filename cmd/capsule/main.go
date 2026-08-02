// Command capsule creates lightweight, isolated development environments that
// disappear when you're done.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/martin-k-m/capsule/internal/cli"
)

func main() {
	err := cli.Run(os.Args[1:])
	if err == nil {
		return
	}

	// A command that ran inside the capsule and failed is not a capsule error.
	// Pass its status straight through so `capsule up -- go test` is usable in a
	// shell pipeline or a CI step.
	var exit *cli.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.Code)
	}

	fmt.Fprintln(os.Stderr, "capsule: "+err.Error())
	os.Exit(1)
}
