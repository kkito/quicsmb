package volume

import (
	"testing"

	"quicsmb/internal/cache"
	"quicsmb/internal/chunk"
	"quicsmb/internal/metastore"
)

func testVolSync(t *testing.T) *Volume {
	t.Helper()
	m, err := metastore.Load(t.TempDir()+"/d.json", chunk.DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	v := New(chunk.DefaultBlockSize, cache.New(20<<30, chunk.DefaultBlockSize), m)
	return v
}

func TestSetFileListRoundTrip(t *testing.T) {
	v := testVolSync(t)
	v.SetFile("sub/nested.txt", 11)   // 远端同步
	v.SetFile("sub/nested.txt", 5)    // 缩小不覆盖
	if v.Size("sub/nested.txt") != 11 {
		t.Fatalf("size = %d", v.Size("sub/nested.txt"))
	}
	files := v.Files()
	if files["sub/nested.txt"] != 11 {
		t.Fatalf("files = %+v", files)
	}
}

func TestTouchDirtyEmptyFile(t *testing.T) {
	v := testVolSync(t)
	// 空文件（O_TRUNC 产物）：无脏块也要标一块，保证回写感知
	if err := v.TouchDirty("empty.txt", 123); err != nil {
		t.Fatal(err)
	}
	if v.DirtyCount() != 1 {
		t.Fatalf("dirty = %d", v.DirtyCount())
	}
}
