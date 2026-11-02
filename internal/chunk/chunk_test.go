package chunk

import "testing"

func TestSplitAndRange(t *testing.T) {
	ids := Split(512<<10+100, 512<<10)
	if len(ids) != 2 || ids[0].Index != 0 || ids[1].Index != 1 {
		t.Fatalf("split = %+v", ids)
	}
	if IndexAt(512<<10+5, 512<<10) != 1 {
		t.Fatal("IndexAt wrong")
	}
	off, ln := RangeOf(1, 512<<10)
	if off != 512<<10 || ln != 512<<10 {
		t.Fatalf("range = %d,%d", off, ln)
	}
	if Count(0, 512<<10) != 0 || Count(1, 512<<10) != 1 {
		t.Fatal("Count wrong")
	}
}

func TestValidBlockSize(t *testing.T) {
	for _, n := range []int{256 << 10, 512 << 10, 1 << 20, 4 << 20} {
		if !ValidBlockSize(n) {
			t.Fatalf("%d should be valid", n)
		}
	}
	if ValidBlockSize(100) || ValidBlockSize(0) || ValidBlockSize(-1) {
		t.Fatal("100/0/-1 should be invalid")
	}
}

func TestChecksumStable(t *testing.T) {
	a := Checksum([]byte("hello"))
	b := Checksum([]byte("hello"))
	c := Checksum([]byte("hellp"))
	if a != b || a == c {
		t.Fatal("checksum unstable or colliding")
	}
}
