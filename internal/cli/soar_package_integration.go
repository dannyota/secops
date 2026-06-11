package cli

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type integrationPackageFile struct {
	Abs  string
	Name string
	Info os.FileInfo
}

type integrationPackageResult struct {
	Output   string   `json:"output"`
	Files    int      `json:"files"`
	Warnings []string `json:"warnings,omitempty"`
}

func newSOARPackageIntegrationCmd() *cobra.Command {
	var (
		outPath string
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "package-integration <dir>",
		Short: "Package a local SOAR custom integration directory into a ZIP (offline)",
		Long: "Package an already-shaped SOAR custom integration directory into a\n" +
			"deterministic ZIP for IDE import. This command is offline: it does not\n" +
			"validate against the tenant and does not mutate SOAR.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := packageSOARIntegrationDir(args[0], outPath, force)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Fprintf(os.Stdout, "Wrote %s (%d file(s)).\n", res.Output, res.Files)
			for _, warn := range res.Warnings {
				fmt.Fprintf(os.Stdout, "warning: %s\n", warn)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&outPath, "out", "", "output ZIP path (default: <dir>.zip)")
	f.BoolVar(&force, "force", false, "overwrite an existing output ZIP")
	return markJSON(cmd)
}

func packageSOARIntegrationDir(dir, outPath string, force bool) (integrationPackageResult, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return integrationPackageResult{}, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return integrationPackageResult{}, err
	}
	if !st.IsDir() {
		return integrationPackageResult{}, fmt.Errorf("%s is not a directory", dir)
	}

	if outPath == "" {
		outPath = root + ".zip"
	}
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		return integrationPackageResult{}, err
	}
	if !force {
		if _, statErr := os.Stat(outAbs); statErr == nil {
			return integrationPackageResult{}, fmt.Errorf("%s already exists (use --force to overwrite)", outAbs)
		} else if !os.IsNotExist(statErr) {
			return integrationPackageResult{}, statErr
		}
	}

	files, err := integrationPackageFiles(root, outAbs)
	if err != nil {
		return integrationPackageResult{}, err
	}
	warnings, err := validateIntegrationPackageFiles(files)
	if err != nil {
		return integrationPackageResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(outAbs), 0o750); err != nil {
		return integrationPackageResult{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(outAbs), "."+filepath.Base(outAbs)+".tmp-*")
	if err != nil {
		return integrationPackageResult{}, err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := writeIntegrationZip(tmp, files); err != nil {
		_ = tmp.Close()
		return integrationPackageResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return integrationPackageResult{}, err
	}
	if force {
		if err := os.Remove(outAbs); err != nil && !os.IsNotExist(err) {
			return integrationPackageResult{}, err
		}
	}
	if err := os.Rename(tmpName, outAbs); err != nil {
		return integrationPackageResult{}, err
	}
	removeTmp = false

	return integrationPackageResult{Output: outAbs, Files: len(files), Warnings: warnings}, nil
}

func integrationPackageFiles(root, outAbs string) ([]integrationPackageFile, error) {
	var files []integrationPackageFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			if shouldSkipIntegrationPackagePath(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipIntegrationPackagePath(d.Name()) {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if samePath(abs, outAbs) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink %s; copy the target into the package directory", path)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !validZipEntryName(name) {
			return fmt.Errorf("invalid package path %q", name)
		}
		files = append(files, integrationPackageFile{Abs: abs, Name: name, Info: info})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func validateIntegrationPackageFiles(files []integrationPackageFile) ([]string, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("package directory has no regular files")
	}
	var hasJSON, hasPython bool
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		hasJSON = hasJSON || ext == ".json"
		hasPython = hasPython || ext == ".py"
	}
	if !hasJSON {
		return nil, fmt.Errorf("package directory has no JSON definition/manifest file")
	}
	var warnings []string
	if !hasPython {
		warnings = append(warnings, "package contains no .py files; confirm this is a definition-only export")
	}
	return warnings, nil
}

func writeIntegrationZip(w io.Writer, files []integrationPackageFile) error {
	zw := zip.NewWriter(w)
	for _, f := range files {
		hdr, err := zip.FileInfoHeader(f.Info)
		if err != nil {
			_ = zw.Close()
			return err
		}
		hdr.Name = f.Name
		hdr.Method = zip.Deflate
		hdr.Modified = zipEpoch()
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = zw.Close()
			return err
		}
		src, err := os.Open(f.Abs)
		if err != nil {
			_ = zw.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := src.Close()
		if copyErr != nil {
			_ = zw.Close()
			return copyErr
		}
		if closeErr != nil {
			_ = zw.Close()
			return closeErr
		}
	}
	return zw.Close()
}

func shouldSkipIntegrationPackagePath(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "__MACOSX", ".DS_Store":
		return true
	default:
		return false
	}
}

func validZipEntryName(name string) bool {
	return name != "" && name != "." && !strings.HasPrefix(name, "../") &&
		!strings.HasPrefix(name, "/") && !strings.Contains(name, "/../")
}

func samePath(a, b string) bool {
	ar, aerr := filepath.EvalSymlinks(a)
	br, berr := filepath.EvalSymlinks(b)
	if aerr == nil && berr == nil {
		return ar == br
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func zipEpoch() time.Time {
	return time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
}
