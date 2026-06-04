// Command secopsctl operates a Google SecOps (Chronicle) instance as code.
package main

import (
	"os"

	"danny.vn/secops/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
