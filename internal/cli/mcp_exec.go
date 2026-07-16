package cli

// Subprocess execution for the MCP server: every tool call runs the
// secopsctl binary itself as a child process. Stdout streams through an
// overflow writer so an oversized result spills to a temp file instead of
// flooding the transport — MCP clients cap tool-result sizes and reject an
// over-limit inline result wholesale, losing the data entirely.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// mcpSpillDefaultBytes is the default inline-result ceiling. 64 KiB of
	// JSON stays under common MCP client tool-result caps (~25K tokens);
	// anything larger goes to a spill file the agent reads or filters.
	mcpSpillDefaultBytes = 64 << 10
	// mcpSpillHeadBytes is the preview length carried inside a spill pointer.
	mcpSpillHeadBytes = 1 << 10
	// mcpSpillMaxAge is how long spill files survive before being swept.
	mcpSpillMaxAge = 24 * time.Hour
)

// mcpSpillThreshold returns the inline ceiling in bytes.
// SECOPS_MCP_SPILL_BYTES overrides the default when set to a positive integer
// (MCP clients differ in how large a tool result they accept).
func mcpSpillThreshold() int {
	if v, err := strconv.Atoi(os.Getenv("SECOPS_MCP_SPILL_BYTES")); err == nil && v > 0 {
		return v
	}
	return mcpSpillDefaultBytes
}

// mcpRunSelf executes the secopsctl binary with argv and returns the tool
// result text. Stdout and stderr are captured separately so diagnostics never
// corrupt a JSON result: success returns stdout (or stderr when stdout is
// empty — some commands emit only warnings); failure returns stderr plus the
// stdout head, with the exit cause appended. spilled reports that out is a
// spill-file pointer rather than inline output.
func mcpRunSelf(argv []string) (out string, spilled bool, err error) {
	self, err := os.Executable()
	if err != nil {
		return "", false, fmt.Errorf("cannot find secopsctl binary: %w", err)
	}

	w := &overflowWriter{limit: mcpSpillThreshold()}
	defer w.discard() // no-op on success: spillPointer already closed the file
	var stderr bytes.Buffer

	cmd := exec.Command(self, argv...) //nolint:gosec // self is os.Executable, argv from the tool schema
	cmd.Env = os.Environ()
	cmd.Dir = mcpProjectDir
	cmd.Stdout = w
	cmd.Stderr = &stderr

	if execErr := cmd.Run(); execErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if head := strings.TrimSpace(w.buf.String()); head != "" {
			if msg != "" {
				msg += "\n"
			}
			msg += head
		}
		msg = mcpCapText(msg, w.limit) // cap first so the cause survives
		if msg == "" {
			msg = execErr.Error()
		} else {
			msg += "\n(" + execErr.Error() + ")"
		}
		return "", false, errors.New(msg)
	}

	if w.file != nil {
		ptr, perr := w.spillPointer()
		if perr != nil {
			return "", false, perr
		}
		return ptr, true, nil
	}
	if len(bytes.TrimSpace(w.buf.Bytes())) == 0 {
		// A successful command may print only diagnostics to stderr;
		// surface those rather than an empty result.
		return strings.TrimSpace(stderr.String()), false, nil
	}
	return w.buf.String(), false, nil
}

// overflowWriter buffers subprocess stdout up to limit bytes in memory; the
// write that would exceed the limit creates a spill file, flushes the
// buffered bytes there, and streams every later write to the file. Memory
// stays bounded no matter how much the subprocess prints. buf always holds
// the first ≤limit bytes of the stream, so head previews and failure output
// work whether or not the output spilled.
type overflowWriter struct {
	limit int
	total int
	buf   bytes.Buffer
	file  *os.File
}

func (w *overflowWriter) Write(p []byte) (int, error) {
	if w.file == nil {
		if w.buf.Len()+len(p) <= w.limit {
			w.total += len(p)
			return w.buf.Write(p)
		}
		f, err := mcpCreateSpillFile()
		if err != nil {
			return 0, err
		}
		w.file = f
		if _, err := w.file.Write(w.buf.Bytes()); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.total += n
	// Keep filling the in-memory head; p already went to the file in full.
	if room := w.limit - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(room, n)])
	}
	return n, err
}

// head returns the first mcpSpillHeadBytes of captured stdout, cut on a rune
// boundary.
func (w *overflowWriter) head() string {
	return runeSafeCut(w.buf.String(), mcpSpillHeadBytes)
}

// spillPointer closes the spill file and returns the JSON tool result that
// points at it: the path, total size, a head preview, and guidance.
func (w *overflowWriter) spillPointer() (string, error) {
	name := w.file.Name()
	err := w.file.Close()
	w.file = nil
	if err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("output too large for an inline result (%d bytes) and the spill file failed: %w", w.total, err)
	}
	ptr, _ := json.Marshal(map[string]any{
		"spilled": true,
		"file":    name,
		"bytes":   w.total,
		"head":    w.head(),
		"message": fmt.Sprintf(
			"Output exceeded the %d-byte inline limit; the full result is in the file above (swept after %dh). Read or filter it (e.g. jq), or re-run with --limit / narrower filters.",
			w.limit, int(mcpSpillMaxAge.Hours())),
	})
	return string(ptr), nil
}

// discard removes the spill file after a failed run — failure diagnostics
// stay inline, and keeping partial bulk output would only orphan temp files.
func (w *overflowWriter) discard() {
	if w.file == nil {
		return
	}
	name := w.file.Name()
	_ = w.file.Close()
	_ = os.Remove(name)
	w.file = nil
}

func mcpSpillDir() string {
	return filepath.Join(os.TempDir(), "secopsctl-mcp")
}

// mcpCreateSpillFile sweeps aged spill files, then creates a fresh one.
func mcpCreateSpillFile() (*os.File, error) {
	dir := mcpSpillDir()
	mcpSweepSpills(dir, mcpSpillMaxAge)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create spill dir: %w", err)
	}
	return os.CreateTemp(dir, "secopsctl-mcp-*.json")
}

// mcpSweepSpills removes spill files older than maxAge so bulk output does
// not accumulate across sessions. Fresh files are never candidates, so a
// sweep racing a concurrent spill is harmless.
func mcpSweepSpills(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// runeSafeCut truncates s to at most n bytes without splitting a UTF-8 rune.
func runeSafeCut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// mcpCapText bounds failure text so oversized output cannot flood the client.
func mcpCapText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := runeSafeCut(s, n)
	return fmt.Sprintf("%s\n[output truncated: %d of %d bytes shown]", cut, len(cut), len(s))
}
