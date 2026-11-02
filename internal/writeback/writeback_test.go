package writeback

import "testing"

func atDay(min int) Clock {
	return func() int64 { return int64(min) * 60 }
}

func TestDueImmediateByDefault(t *testing.T) {
	p := DefaultPolicy()
	old := DirtyEntryView{DirtyUnix: 0}
	// 默认即时同步：刚变脏、下个 tick 就 due，全天任何时间都 due
	if !Due(old, p, atDay(12*60)) {
		t.Fatal("default should be due at noon")
	}
	if !Due(DirtyEntryView{DirtyUnix: int64(12*60)*60 - 5}, p, atDay(12*60)) {
		t.Fatal("fresh entry should be due immediately")
	}
	// 失败超限不 due
	if Due(DirtyEntryView{DirtyUnix: 0, Failures: 99}, p, atDay(150)) {
		t.Fatal("over-failure should not be due")
	}
}

func TestDueDelayedPolicy(t *testing.T) {
	// 延迟回写通过参数覆盖（错峰场景）：满延时 + 窗口内才 due
	p := Policy{DelaySec: 72 * 3600, WinStartMin: 120, WinEndMin: 180, MaxFailures: 10}
	old := DirtyEntryView{DirtyUnix: 0}
	// 72h 整 + 窗口内 02:30
	if !Due(old, p, atDay(3*24*60+150)) {
		t.Fatal("72h+window should be due")
	}
	// 未满 72h 即使窗口内也不 due（1 小时前才变脏）
	nowMin := 3*24*60 + 150
	if Due(DirtyEntryView{DirtyUnix: int64(nowMin)*60 - 3600}, p, atDay(nowMin)) {
		t.Fatal("fresh entry should not be due")
	}
	// 满 72h 但窗口外（12:00）不 due
	if Due(old, p, atDay(3*24*60+12*60)) {
		t.Fatal("outside window should not be due")
	}
}

func TestSelectDueOldestFirst(t *testing.T) {
	p := DefaultPolicy()
	all := []DirtyEntryView{{DirtyUnix: 200}, {DirtyUnix: 100}, {DirtyUnix: 50}}
	idx := SelectDue(all, p, atDay(30*24*60+150), 2)
	if len(idx) != 2 || idx[0] != 2 || idx[1] != 1 {
		t.Fatalf("idx = %v", idx)
	}
}
