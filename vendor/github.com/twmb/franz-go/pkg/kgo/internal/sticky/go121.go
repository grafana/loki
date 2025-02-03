//go:build go1.21
// +build go1.21

package sticky

import "slices"

func sortPartNums(ps memberPartitions) {
	slices.Sort(ps)
}

func (b *balancer) sortMemberByLiteralPartNum(memberNum int) {
	partNums := b.plan[memberNum]
	slices.SortFunc(partNums, func(lpNum, rpNum int32) int {
		ltNum, rtNum := b.partOwners[lpNum], b.partOwners[rpNum]
		li, ri := b.topicInfos[ltNum], b.topicInfos[rtNum]
		lt, rt := li.topic, ri.topic
		lp, rp := lpNum-li.partNum, rpNum-ri.partNum
		if lp < rp {
			return -1
		} else if lp > rp {
			return 1
		} else if lt < rt {
			return -1
		}
		return 1
	})
}
