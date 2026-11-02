package metastore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCorruptAndSaveErrors(t *testing.T) {
	// 损坏的 JSON → Load 报错
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad, 512<<10); err == nil {
		t.Fatal("want error on corrupt json")
	}
	// 不存在的路径 → Load 返回空 store 不报错
	s, err := Load(filepath.Join(t.TempDir(), "nope", "d.json"), 512<<10)
	if err != nil || len(s.List()) != 0 {
		t.Fatalf("load missing = %v %+v", err, s.List())
	}
	// 同 key 重复 MarkDirty 只更新 sum（覆盖 MarkDirty 已存在分支）
	p := filepath.Join(t.TempDir(), "d.json")
	s2, _ := Load(p, 512<<10)
	s2.MarkDirty("f", 0, 1, 100)
	s2.MarkDirty("f", 0, 2, 200)
	if got := s2.List(); len(got) != 1 || got[0].Sum != 2 {
		t.Fatalf("redirty = %+v", got)
	}
	// BumpFailure 不存在的 key：静默无 panic
	s2.BumpFailure("ghost", 9)
	// Save 到非法路径 → 报错
	s3 := New(filepath.Join(t.TempDir(), "notexist", "sub", "d.json"), 512<<10)
	s3.MarkDirty("f", 0, 1, 1)
	// 父目录不存在时 WriteFile(tmp) 失败 → Save 报错
	badStore := New("/proc/no-such-dir-xyz/d.json", 512<<10)
	badStore.MarkDirty("f", 0, 1, 1)
	if err := badStore.Save(); err == nil {
		t.Fatal("want save error on bad path")
	}
}
