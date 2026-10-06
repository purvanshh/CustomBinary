// Package pathsafe resolves request paths under a document root without traversal.
package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrTraversal = errors.New("pathsafe: path escapes document root")
	ErrEmptyPath = errors.New("pathsafe: empty path")
)

// Resolve maps a request path onto root, rejecting traversal.
// Returns the absolute filesystem path on success.
func Resolve(root, reqPath string) (string, error) {
	if reqPath == "" {
		return "", ErrEmptyPath
	}

	absRoot, err := canonicalDir(root)
	if err != nil {
		return "", err
	}

	p := reqPath
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	// Reject NUL and other control characters in paths.
	if strings.ContainsRune(p, 0) {
		return "", ErrTraversal
	}

	cleaned := filepath.Clean(p)
	rel := strings.TrimPrefix(cleaned, string(os.PathSeparator))
	if cleaned == string(os.PathSeparator) || rel == "" || rel == "." {
		rel = "index.html"
	}

	// Refuse any remaining ".." segment after Clean (defensive).
	for _, seg := range strings.Split(rel, string(os.PathSeparator)) {
		if seg == ".." {
			return "", ErrTraversal
		}
	}

	candidate := filepath.Join(absRoot, rel)
	candidate = filepath.Clean(candidate)

	if !containedIn(absRoot, candidate) {
		return "", ErrTraversal
	}

	// If the path exists (file or symlink), resolve symlinks and re-check.
	if fi, err := os.Lstat(candidate); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", ErrTraversal
			}
			resolved = filepath.Clean(resolved)
			if !containedIn(absRoot, resolved) {
				return "", ErrTraversal
			}
			return resolved, nil
		}
	}

	return candidate, nil
}

func canonicalDir(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", errors.New("pathsafe: root is not a directory")
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func containedIn(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	sep := string(os.PathSeparator)
	prefix := root
	if !strings.HasSuffix(prefix, sep) {
		prefix += sep
	}
	return path == root || strings.HasPrefix(path, prefix)
}
