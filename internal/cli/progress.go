package cli

import (
	"fmt"
	"os"
)

var noProgress bool

func stderrIsTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printProgress(resource string, fetched, total int) {
	if noProgress || jsonOut || !stderrIsTTY() {
		return
	}
	if total > 0 {
		fmt.Fprintf(os.Stderr, "\rfetching %s… %d/%d", resource, fetched, total)
	} else {
		fmt.Fprintf(os.Stderr, "\rfetching %s… %d", resource, fetched)
	}
}

func clearProgress() {
	if noProgress || jsonOut || !stderrIsTTY() {
		return
	}
	fmt.Fprint(os.Stderr, "\r\033[K")
}
