//go:build !go1.21
// +build !go1.21

package sticky

import "sort"

func sortPartNums(partNums memberPartitions) {
	sort.Slice(partNums, func(i, j int) bool { return partNums[i] < partNums[j] })
}

func (b *balancer) sortMemberByLiteralPartNum(memberNum int) {
	partNums := b.plan[memberNum]
	sort.Slice(partNums, func(i, j int) bool {
		lpNum, rpNum := partNums[i], partNums[j]
		ltNum, rtNum := b.partOwners[lpNum], b.partOwners[rpNum]
		li, ri := b.topicInfos[ltNum], b.topicInfos[rtNum]
		lt, rt := li.topic, ri.topic
		lp, rp := lpNum-li.partNum, rpNum-ri.partNum
		return lp < rp || (lp == rp && lt < rt)
	})
}
