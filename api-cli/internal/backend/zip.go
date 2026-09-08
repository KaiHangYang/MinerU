package backend

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractZip unpacks src into destDir (created if needed) and returns the
// paths of every regular file it wrote. It refuses entries that would escape
// destDir via ".." path segments.
//
// If rewrite is non-nil, it's applied to each entry's path before joining it
// onto destDir; an entry that rewrites to "" is skipped entirely (used to
// drop directory entries that become empty after stripping path segments).
func extractZip(src, destDir string, rewrite func(name string) string) ([]string, error) {
	r, err := zip.OpenReader(src)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	var written []string
	for _, f := range r.File {
		name := f.Name
		if rewrite != nil {
			name = rewrite(name)
			if name == "" {
				continue
			}
		}
		target := filepath.Join(destDir, name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) && target != filepath.Clean(destDir) {
			return written, fmt.Errorf("zip entry escapes output dir: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return written, err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return written, err
		}

		rc, err := f.Open()
		if err != nil {
			return written, err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return written, err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return written, copyErr
		}
		written = append(written, target)
	}
	return written, nil
}
