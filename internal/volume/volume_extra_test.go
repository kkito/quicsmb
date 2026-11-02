package volume

import (
	"testing"

	"quicsmb/internal/cache"
	"quicsmb/internal/chunk"
	"quicsmb/internal/metastore"
)

// Size/Usage/Capacity/HitRate/DirtyCount/Truncate/TouchDirty 全覆盖。
func TestVolumeStatsAndTruncate(t *testing.T) {
	meta, err := metastore.Load(t.TempDir()+"/d.json", 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	v := New(512<<10, cache.New(20<<30, 512<<10), meta)
	if v.Size("nofile") != 0 {
		t.Fatal("empty size must be 0")
	}
	used, n := v.Usage()
	if used != 0 || n != 0 {
		t.Fatalf("empty usage = %d/%d", used, n)
	}
	if v.HitRate() != 0 || v.DirtyCount() != 0 {
		t.Fatal("empty stats must be 0")
	}
	v.SetCapacity(100, 40)
	if total, free := v.Capacity(); total != 100 || free != 40 {
		t.Fatalf("capacity = %d/%d", total, free)
	}
	if _, err := v.WriteAt("a.bin", 0, []byte("hi"), 100); err != nil {
		t.Fatal(err)
	}
	if v.Size("a.bin") != 2 || v.DirtyCount() != 1 {
		t.Fatalf("size/dirty = %d/%d", v.Size("a.bin"), v.DirtyCount())
	}
	if _, n := v.Usage(); n != 1 {
		t.Fatalf("blocks = %d", n)
	}
	v.Truncate("a.bin", 1)
	if v.Size("a.bin") != 1 {
		t.Fatalf("truncated = %d", v.Size("a.bin"))
	}
	v.Truncate("neg", -5)
	if v.Size("neg") != 0 {
		t.Fatal("negative truncate must clamp 0")
	}
	// TouchDirty：脏块再标脏（幂等），无脏文件不报错
	if err := v.TouchDirty("a.bin", 200); err != nil {
		t.Fatal(err)
	}
	if err := v.TouchDirty("ghost", 200); err != nil {
		t.Fatal(err)
	}
	// 命中一次 HitRate > 0
	buf := make([]byte, 2)
	nofetch := func(f string, i uint64) ([]byte, error) { return make([]byte, 512<<10), nil }
	if _, err := v.ReadAt("a.bin", 0, buf, nofetch); err != nil {
		t.Fatal(err)
	}
	if v.HitRate() <= 0 {
		t.Fatal("hitrate must be > 0 after hit")
	}
	_ = chunk.DefaultBlockSize
}
