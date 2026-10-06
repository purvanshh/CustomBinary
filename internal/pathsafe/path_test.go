package pathsafe_test

import (
	"os"
	"path/filepath"
	"testing"

	"bp/internal/pathsafe"
)

func TestResolveOK(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := pathsafe.Resolve(root, "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil || string(data) != "hi" {
		t.Fatalf("%v %q", err, data)
	}
}

func TestTraversalNeverReadsOutside(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "www")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(parent, "secret")
	if err := os.WriteFile(secret, []byte("LEAK"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		"/../../etc/passwd",
		"/../secret",
		"/foo/../../secret",
		"/../../secret",
	} {
		resolved, err := pathsafe.Resolve(root, p)
		if err == pathsafe.ErrTraversal {
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		// Resolved path must not be the outside secret file.
		if sameFile(resolved, secret) {
			t.Fatalf("path %q resolved to secret %q", p, resolved)
		}
		data, err := os.ReadFile(resolved)
		if err == nil && string(data) == "LEAK" {
			t.Fatalf("path %q leaked secret via %q", p, resolved)
		}
	}
}

func sameFile(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func TestSymlinkEscapeRejected(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "www")
	_ = os.Mkdir(root, 0755)
	secret := filepath.Join(parent, "secret")
	_ = os.WriteFile(secret, []byte("LEAK"), 0644)
	link := filepath.Join(root, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("symlink not supported:", err)
	}
	_, err := pathsafe.Resolve(root, "/link")
	if err != pathsafe.ErrTraversal {
		t.Fatalf("want ErrTraversal got %v", err)
	}
}
