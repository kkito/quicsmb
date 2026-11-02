// Package writeback 是回写调度的纯判定：满延时 + 低峰窗口 + 失败上限。
package writeback

import "sort"

type Clock func() int64

type Policy struct {
	DelaySec    int64
	WinStartMin int
	WinEndMin   int
	MaxFailures int
}

type DirtyEntryView struct {
	DirtyUnix int64
	Failures  int
}

func DefaultPolicy() Policy {
	// 默认即时同步：脏块产生后下个 tick 就推，全天窗口。
	// 如需延迟回写（错峰），用 QVFS_DELAY_SEC / QVFS_WIN 覆盖。
	return Policy{DelaySec: 0, WinStartMin: 0, WinEndMin: 1440, MaxFailures: 10}
}

func inWindow(nowUnix int64, p Policy) bool {
	m := int((nowUnix / 60) % (24 * 60))
	if p.WinStartMin <= p.WinEndMin {
		return m >= p.WinStartMin && m < p.WinEndMin
	}
	return m >= p.WinStartMin || m < p.WinEndMin
}

func Due(e DirtyEntryView, p Policy, now Clock) bool {
	t := now()
	if e.Failures >= p.MaxFailures {
		return false
	}
	if t-e.DirtyUnix < p.DelaySec {
		return false
	}
	return inWindow(t, p)
}

func SelectDue(all []DirtyEntryView, p Policy, now Clock, limit int) []int {
	var idx []int
	for i, e := range all {
		if Due(e, p, now) {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool {
		return all[idx[a]].DirtyUnix < all[idx[b]].DirtyUnix
	})
	if limit > 0 && len(idx) > limit {
		idx = idx[:limit]
	}
	return idx
}
