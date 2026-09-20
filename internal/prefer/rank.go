package prefer

import (
	"fmt"
	"sort"
)

// RankTop 从探测结果中选出可达并按延迟升序（速度降序做次级排序）取前 TopN。
func RankTop(results []Result, topN int) []Ranked {
	if topN <= 0 {
		topN = 1
	}
	var ok []Result
	for _, r := range results {
		if r.OK {
			ok = append(ok, r)
		}
	}
	sort.SliceStable(ok, func(i, j int) bool {
		a, b := ok[i], ok[j]
		aT, bT := a.SpeedMbps > 0, b.SpeedMbps > 0
		switch {
		case aT && bT:
			if a.SpeedMbps != b.SpeedMbps {
				return a.SpeedMbps > b.SpeedMbps
			}
			return a.Delay < b.Delay
		case aT:
			return true // 测出速度的排前
		case bT:
			return false
		default:
			return a.Delay < b.Delay
		}
	})
	if len(ok) > topN {
		ok = ok[:topN]
	}
	out := make([]Ranked, 0, len(ok))
	for _, r := range ok {
		out = append(out, Ranked{
			Upstream:  fmt.Sprintf("https://%s", r.IP),
			IP:        r.IP,
			DelayMS:   float64(r.Delay.Microseconds()) / 1000,
			SpeedMbps: r.SpeedMbps,
		})
	}
	return out
}
