package disk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSkipsDockerAndDenied(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	r := New(root)
	if err := r.WriteAt("docker/layer.bin", 0, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteAt("proj/a.txt", 0, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "noperm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noperm", "s.txt"), []byte("s"), 0o000); err != nil {
		t.Fatal(err)
	}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range list {
		got[e.Name] = true
	}
	if got["docker/layer.bin"] {
		t.Fatalf("docker should be skipped: %+v", list)
	}
	if !got["proj/a.txt"] {
		t.Fatalf("proj should be listed: %+v", list)
	}
}
