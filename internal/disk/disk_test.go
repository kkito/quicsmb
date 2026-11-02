package disk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "root"))
	if err := r.WriteAt("a/b.bin", 10, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadAt("a/b.bin", 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	if sz := r.Size("a/b.bin"); sz != 15 {
		t.Fatalf("size = %d", sz)
	}
}

func TestSparseReadZeroFilled(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "root"))
	got, err := r.ReadAt("nope.bin", 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range got {
		if b != 0 {
			t.Fatalf("byte %d = %d, want 0", i, b)
		}
	}
	if sz := r.Size("nope.bin"); sz != 0 {
		t.Fatalf("size = %d", sz)
	}
	// 文件存在但越界读
	if err := r.WriteAt("s.bin", 0, []byte("ab")); err != nil {
		t.Fatal(err)
	}
	got, err = r.ReadAt("s.bin", 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 'b' || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("oob = %v", got)
	}
	_ = os.MkdirAll(".", 0o755)
}

func TestPathEscapeRejected(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "root"))
	for _, p := range []string{"../evil", "/abs", "a/../../evil"} {
		if err := r.WriteAt(p, 0, []byte("x")); err == nil {
			t.Fatalf("write %q must fail", p)
		}
		if _, err := r.ReadAt(p, 0, 1); err == nil {
			t.Fatalf("read %q must fail", p)
		}
	}
}
