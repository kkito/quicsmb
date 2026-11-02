package disk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTruncateShrinks(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "root"))
	if err := r.WriteAt("f.bin", 0, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if err := r.Truncate("f.bin", 26); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(r.Abs(), "f.bin"))
	if st.Size() != 26 {
		t.Fatalf("size = %d", st.Size())
	}
	// 不存在的父目录自动建
	if err := r.Truncate("sub/new.bin", 0); err != nil {
		t.Fatal(err)
	}
	// 逃逸拒绝
	if err := r.Truncate("../evil", 0); err == nil {
		t.Fatal("want escape error")
	}
}
