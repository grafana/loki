package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(35, 0, 4) }

func (c *Cluster) handleDescribeLogDirs(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.DescribeLogDirsRequest)
	resp := req.ResponseKind().(*kmsg.DescribeLogDirsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	totalSpace := make(map[string]int64)
	individual := make(map[string]map[string]map[int32]int64)

	add := func(d string, t string, p int32, s int64) {
		totalSpace[d] += s
		ts, ok := individual[d]
		if !ok {
			ts = make(map[string]map[int32]int64)
			individual[d] = ts
		}
		ps, ok := ts[t]
		if !ok {
			ps = make(map[int32]int64)
			ts[t] = ps
		}
		ps[p] += s
	}

	if req.Topics == nil {
		c.data.tps.each(func(t string, p int32, d *partData) {
			add(d.dir, t, p, d.nbytes)
		})
	} else {
		for _, t := range req.Topics {
			for _, p := range t.Partitions {
				d, ok := c.data.tps.getp(t.Topic, p)
				if ok {
					add(d.dir, t.Topic, p, d.nbytes)
				}
			}
		}
	}

	for dir, ts := range individual {
		rd := kmsg.NewDescribeLogDirsResponseDir()
		rd.Dir = dir
		rd.TotalBytes = totalSpace[dir]
		rd.UsableBytes = 32 << 30
		for t, ps := range ts {
			rt := kmsg.NewDescribeLogDirsResponseDirTopic()
			rt.Topic = t
			for p, s := range ps {
				rp := kmsg.NewDescribeLogDirsResponseDirTopicPartition()
				rp.Partition = p
				rp.Size = s
				rt.Partitions = append(rt.Partitions, rp)
			}
			rd.Topics = append(rd.Topics, rt)
		}
		resp.Dirs = append(resp.Dirs, rd)
	}

	return resp, nil
}
