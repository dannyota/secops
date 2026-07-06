package cli

import (
	"fmt"
	"os"
	"time"
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

// progressTicker starts a background goroutine that updates stderr with elapsed
// time every 2s while a long-running blocking call is in progress. Call the
// returned stop function when the call completes — it clears the progress line.
// The ticker respects --no-progress, --json, and non-TTY stderr (no-op).
func progressTicker(label string) (stop func()) {
	if noProgress || jsonOut || !stderrIsTTY() {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		start := time.Now()
		fmt.Fprintf(os.Stderr, "\rfetching %s…", label)
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				elapsed := time.Since(start).Truncate(time.Second)
				fmt.Fprintf(os.Stderr, "\rfetching %s… %s", label, elapsed)
			}
		}
	}()
	return func() {
		close(done)
		clearProgress()
	}
}

func clearProgress() {
	if noProgress || jsonOut || !stderrIsTTY() {
		return
	}
	fmt.Fprint(os.Stderr, "\r\033[K")
}
