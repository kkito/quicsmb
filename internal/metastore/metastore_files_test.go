package metastore

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRaw(p, s string) error { return os.WriteFile(p, []byte(s), 0600) }

func TestFilesSetAndRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.json")
	s, _ := Load(p, 512<<10)
	// 只放大不缩小
	s.SetFile("a.bin", 100)
	s.SetFile("a.bin", 50)
	s.SetFile("a.bin", 200)
	if got := s.Files(); got["a.bin"] != 200 {
		t.Fatalf("files = %+v", got)
	}
	if !s.IsDirty("a.bin", 0) {
		// 还没标脏：false
	}
	s.MarkDirty("a.bin", 3, 1, 1)
	if !s.IsDirty("a.bin", 3) || s.IsDirty("a.bin", 4) {
		t.Fatal("IsDirty mismatch")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(p, 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Files(); got["a.bin"] != 200 {
		t.Fatalf("reload files = %+v", got)
	}
	// 老版本无 files 段：脏块反推文件存在
	s3, _ := Load(p, 512<<10)
	_ = s3
}

func TestLoadLegacyDirtyWithoutFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "legacy.json")
	s, _ := Load(p, 512<<10)
	s.MarkDirty("legacy.bin", 0, 7, 9)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// 手工抹掉 files 段模拟老版本
	raw := `{"block_size":524288,"entries":[{"file":"legacy.bin","index":0,"dirty_unix":9,"sum":7,"failures":0}]}`
	if err := writeRaw(p, raw); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(p, 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Files()["legacy.bin"]; !ok {
		t.Fatalf("legacy recovery = %+v", s2.Files())
	}
}
