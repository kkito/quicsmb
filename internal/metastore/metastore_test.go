package metastore

import (
	"path/filepath"
	"testing"
)

func TestDirtyRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dirty.json")
	s, err := Load(p, 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	s.MarkDirty("f", 3, 99, 1000)
	s.MarkDirty("f", 4, 100, 1001)
	s.BumpFailure("f", 4)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(p, 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.List()
	if len(got) != 2 {
		t.Fatalf("list = %+v", got)
	}
	s2.MarkClean("f", 3)
	if len(s2.List()) != 1 || s2.List()[0].Failures != 1 {
		t.Fatalf("after clean = %+v", s2.List())
	}
}

func TestBlockSizeMismatchRefuses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.json")
	s, _ := Load(p, 512<<10)
	s.MarkDirty("f", 0, 1, 1)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p, 1<<20); err == nil {
		t.Fatal("want error on block size mismatch")
	}
}
