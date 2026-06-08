package cli

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPackageSOARIntegrationDir(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "manifest.json"), `{"name":"ExampleIntegration"}`)
	mustWriteFile(t, filepath.Join(dir, "Actions", "Ping.py"), "print('ok')\n")
	mustWriteFile(t, filepath.Join(dir, ".DS_Store"), "ignored")
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, ".git", "config"), "ignored")

	out := filepath.Join(t.TempDir(), "ExampleIntegration.zip")
	res, err := packageSOARIntegrationDir(dir, out, false)
	if err != nil {
		t.Fatalf("packageSOARIntegrationDir: %v", err)
	}
	if res.Files != 2 || len(res.Warnings) != 0 {
		t.Fatalf("result = %+v, want 2 files and no warnings", res)
	}

	names, mods := zipEntries(t, out)
	want := []string{"Actions/Ping.py", "manifest.json"}
	if !slices.Equal(names, want) {
		t.Fatalf("zip entries = %v, want %v", names, want)
	}
	for _, mod := range mods {
		if !mod.Equal(zipEpoch()) {
			t.Fatalf("zip timestamp = %s, want deterministic epoch", mod)
		}
	}
}

func TestPackageSOARIntegrationDirRequiresJSON(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Actions", "Ping.py"), "print('ok')\n")

	_, err := packageSOARIntegrationDir(dir, filepath.Join(t.TempDir(), "x.zip"), false)
	if err == nil || !strings.Contains(err.Error(), "no JSON") {
		t.Fatalf("err = %v, want no JSON error", err)
	}
}

func TestPackageSOARIntegrationDirWarnsWithoutPython(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "manifest.json"), `{"name":"ExampleIntegration"}`)

	res, err := packageSOARIntegrationDir(dir, filepath.Join(t.TempDir(), "x.zip"), false)
	if err != nil {
		t.Fatalf("packageSOARIntegrationDir: %v", err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "no .py") {
		t.Fatalf("warnings = %v, want no .py warning", res.Warnings)
	}
}

func TestPackageSOARIntegrationDirOverwriteGuard(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "manifest.json"), `{"name":"ExampleIntegration"}`)
	out := filepath.Join(t.TempDir(), "x.zip")
	mustWriteFile(t, out, "existing")

	if _, err := packageSOARIntegrationDir(dir, out, false); err == nil {
		t.Fatal("expected overwrite guard error")
	}
	if _, err := packageSOARIntegrationDir(dir, out, true); err != nil {
		t.Fatalf("--force package: %v", err)
	}
}

func TestPackageSOARIntegrationDirRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "manifest.json"), `{"name":"ExampleIntegration"}`)
	target := filepath.Join(dir, "target.py")
	mustWriteFile(t, target, "print('ok')\n")
	link := filepath.Join(dir, "Actions", "Ping.py")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := packageSOARIntegrationDir(dir, filepath.Join(t.TempDir(), "x.zip"), false)
	if err == nil || !strings.Contains(err.Error(), "refusing symlink") {
		t.Fatalf("err = %v, want symlink refusal", err)
	}
}

func mustWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zipEntries(t *testing.T, path string) ([]string, []time.Time) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	mods := make([]time.Time, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
		mods = append(mods, f.Modified.UTC())
	}
	return names, mods
}
