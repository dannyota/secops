package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestOverflowWriterInline(t *testing.T) {
	w := &overflowWriter{limit: 100}
	if _, err := w.Write([]byte("hello ")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	if w.file != nil {
		t.Error("under-limit output should not spill")
	}
	if got := w.buf.String(); got != "hello world" {
		t.Errorf("buf = %q", got)
	}
	if w.total != 11 {
		t.Errorf("total = %d, want 11", w.total)
	}
}

func TestOverflowWriterSpill(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	tests := []struct {
		name   string
		limit  int
		writes []string
	}{
		{"crossing writes", 10, []string{"12345", "67890", "abcde"}},
		{"single oversized write", 4, []string{"1234567890"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &overflowWriter{limit: tt.limit}
			var want bytes.Buffer
			for _, p := range tt.writes {
				want.WriteString(p)
				if _, err := w.Write([]byte(p)); err != nil {
					t.Fatal(err)
				}
			}
			if w.file == nil {
				t.Fatal("over-limit output should spill")
			}
			if w.total != want.Len() {
				t.Errorf("total = %d, want %d", w.total, want.Len())
			}
			// buf holds the head even when the first write already overflowed.
			wantHead := want.String()[:min(tt.limit, want.Len())]
			if got := w.buf.String(); got != wantHead {
				t.Errorf("buf = %q, want head %q", got, wantHead)
			}

			name := w.file.Name()
			if err := w.file.Close(); err != nil {
				t.Fatal(err)
			}
			w.file = nil
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, want.Bytes()) {
				t.Errorf("spill file = %q, want %q", data, want.Bytes())
			}
		})
	}
}

func TestSpillPointerRoundTripAndSweep(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	payload := bytes.Repeat([]byte("x"), 4096)
	w := &overflowWriter{limit: 64}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	ptr, err := w.spillPointer()
	if err != nil {
		t.Fatalf("spillPointer: %v", err)
	}

	var meta struct {
		Spilled bool   `json:"spilled"`
		File    string `json:"file"`
		Bytes   int    `json:"bytes"`
		Head    string `json:"head"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(ptr), &meta); err != nil {
		t.Fatalf("pointer is not JSON: %v\n%s", err, ptr)
	}
	if !meta.Spilled || meta.Bytes != len(payload) || meta.Head == "" || meta.Message == "" {
		t.Errorf("pointer fields = %+v", meta)
	}
	data, err := os.ReadFile(meta.File)
	if err != nil {
		t.Fatalf("read spill file: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Error("spill file content differs from output")
	}

	// A file older than the retention window is swept; a fresh one stays.
	old := time.Now().Add(-2 * mcpSpillMaxAge)
	if err := os.Chtimes(meta.File, old, old); err != nil {
		t.Fatal(err)
	}
	w2 := &overflowWriter{limit: 64}
	if _, err := w2.Write(payload); err != nil {
		t.Fatal(err)
	}
	fresh, err := w2.spillPointer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(meta.File); !os.IsNotExist(err) {
		t.Error("expected aged spill file to be swept")
	}
	var freshMeta struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal([]byte(fresh), &freshMeta); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(freshMeta.File); err != nil {
		t.Errorf("fresh spill file should remain: %v", err)
	}
}

func TestMCPSpillThreshold(t *testing.T) {
	tests := []struct {
		env  string
		want int
	}{
		{"", mcpSpillDefaultBytes},
		{"1234", 1234},
		{"0", mcpSpillDefaultBytes},
		{"-5", mcpSpillDefaultBytes},
		{"garbage", mcpSpillDefaultBytes},
	}
	for _, tt := range tests {
		t.Setenv("SECOPS_MCP_SPILL_BYTES", tt.env)
		if got := mcpSpillThreshold(); got != tt.want {
			t.Errorf("SECOPS_MCP_SPILL_BYTES=%q → %d, want %d", tt.env, got, tt.want)
		}
	}
}

func TestRuneSafeCutAndCapText(t *testing.T) {
	multi := strings.Repeat("é", 100) // 2 bytes per rune
	cut := runeSafeCut(multi, 33)
	if !utf8.ValidString(cut) {
		t.Error("cut is not valid UTF-8")
	}
	if len(cut) != 32 {
		t.Errorf("len(cut) = %d, want 32", len(cut))
	}
	if got := runeSafeCut("short", 100); got != "short" {
		t.Errorf("under-limit cut = %q", got)
	}

	capped := mcpCapText(multi, 33)
	if !strings.Contains(capped, "[output truncated: 32 of 200 bytes shown]") {
		t.Errorf("capped = %q", capped)
	}
	if got := mcpCapText("short", 100); got != "short" {
		t.Errorf("under-limit cap = %q", got)
	}
}

// TestMCPHelperProcess is not a real test: mcpRunSelf executes the test
// binary itself, and this hook plays the subprocess role when the env gate
// is set. os.Exit keeps the test framework's own output off the stream.
func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("SECOPS_MCP_TEST_HELPER") == "" {
		t.Skip("helper process hook")
	}
	switch os.Getenv("SECOPS_MCP_TEST_MODE") {
	case "big":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 200_000))
	case "stderr-only":
		fmt.Fprint(os.Stderr, "warning: only diagnostics\n")
	case "fail":
		fmt.Fprint(os.Stderr, "Error: boom\n")
		os.Exit(2)
	case "failbig":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("y"), 100_000))
		os.Exit(1)
	default:
		fmt.Fprint(os.Stdout, `{"ok":true}`)
	}
	os.Exit(0) // keep the framework's own PASS output off the stream
}

func TestMCPRunSelf(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("SECOPS_MCP_TEST_HELPER", "1")
	argv := []string{"-test.run=TestMCPHelperProcess"}

	t.Run("inline success", func(t *testing.T) {
		t.Setenv("SECOPS_MCP_TEST_MODE", "")
		out, spilled, err := mcpRunSelf(argv)
		if err != nil || spilled {
			t.Fatalf("err=%v spilled=%v", err, spilled)
		}
		if out != `{"ok":true}` {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("stderr fallback on empty stdout", func(t *testing.T) {
		t.Setenv("SECOPS_MCP_TEST_MODE", "stderr-only")
		out, spilled, err := mcpRunSelf(argv)
		if err != nil || spilled {
			t.Fatalf("err=%v spilled=%v", err, spilled)
		}
		if out != "warning: only diagnostics" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("large output spills", func(t *testing.T) {
		t.Setenv("SECOPS_MCP_TEST_MODE", "big")
		out, spilled, err := mcpRunSelf(argv)
		if err != nil || !spilled {
			t.Fatalf("err=%v spilled=%v", err, spilled)
		}
		var meta struct {
			File  string `json:"file"`
			Bytes int    `json:"bytes"`
			Head  string `json:"head"`
		}
		if err := json.Unmarshal([]byte(out), &meta); err != nil {
			t.Fatalf("pointer is not JSON: %v\n%s", err, out)
		}
		if meta.Bytes != 200_000 || !strings.HasPrefix(meta.Head, "xxx") {
			t.Errorf("meta = %+v", meta)
		}
		data, err := os.ReadFile(meta.File)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 200_000 {
			t.Errorf("spill file has %d bytes, want 200000", len(data))
		}
	})

	t.Run("failure returns stderr plus cause", func(t *testing.T) {
		t.Setenv("SECOPS_MCP_TEST_MODE", "fail")
		_, _, err := mcpRunSelf(argv)
		if err == nil {
			t.Fatal("want error")
		}
		if !strings.Contains(err.Error(), "Error: boom") || !strings.Contains(err.Error(), "exit status 2") {
			t.Errorf("err = %q", err)
		}
	})

	t.Run("failure discards spill file", func(t *testing.T) {
		own := t.TempDir() // isolated from the successful-spill subtest's file
		t.Setenv("TMPDIR", own)
		t.Setenv("SECOPS_MCP_TEST_MODE", "failbig")
		_, _, err := mcpRunSelf(argv)
		if err == nil {
			t.Fatal("want error")
		}
		if !strings.Contains(err.Error(), "exit status 1") {
			t.Errorf("err = %q", err)
		}
		entries, _ := os.ReadDir(filepath.Join(own, "secopsctl-mcp"))
		if len(entries) != 0 {
			t.Errorf("spill dir should be empty after a failed run, has %d entries", len(entries))
		}
	})
}
