package disk

import (
	"path/filepath"
	"testing"
)

func TestReadDirSingleLevel(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "root"))
	if err := r.WriteAt("sub/a.txt", 0, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteAt("top.bin", 0, []byte("12345")); err != nil {
		t.Fatal(err)
	}
	des, err := r.ReadDir("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]FileEntryDir{}
	for _, de := range des {
		got[de.Name] = de
	}
	if !got["sub"].IsDir || got["top.bin"].Size != 5 || got["top.bin"].IsDir {
		t.Fatalf("readdir = %+v", des)
	}
	sub, err := r.ReadDir("sub")
	if err != nil || len(sub) != 1 || sub[0].Name != "a.txt" || sub[0].Size != 2 {
		t.Fatalf("sub = %+v %v", sub, err)
	}
	// 不存在/逃逸
	if des, err := r.ReadDir("nope"); err != nil || len(des) != 0 {
		t.Fatalf("nope = %+v %v", des, err)
	}
	if _, err := r.ReadDir("../evil"); err == nil {
		t.Fatal("want escape error")
	}
	// Stat
	if isDir, _, found := r.Stat("sub"); !found || !isDir {
		t.Fatal("stat sub")
	}
	if isDir, sz, found := r.Stat("top.bin"); !found || isDir || sz != 5 {
		t.Fatal("stat top.bin")
	}
	if _, _, found := r.Stat("missing"); found {
		t.Fatal("stat missing should not found")
	}
}
