package compactor

import (
	"testing"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

type sizedRun struct {
	path string
	size uint64
}

func (r sizedRun) Sections() []*compactionv2pb.SectionRef {
	return []*compactionv2pb.SectionRef{{ObjectPath: r.path}}
}
func (r sizedRun) Size() uint64 { return r.size }

func taskPaths(tasks []*compactionv2pb.TaskSpec) [][]string {
	out := make([][]string, len(tasks))
	for i, task := range tasks {
		for _, run := range task.Runs {
			out[i] = append(out[i], run.Sections[0].ObjectPath)
		}
	}
	return out
}

func TestPlanLogMergeTasks(t *testing.T) {
	const gib = uint64(1) << 30

	t.Run("does not mix runs from different size levels in one task", func(t *testing.T) {
		runs := []v2.Run{
			sizedRun{"small-a", 1 * gib},
			sizedRun{"large", 50 * gib},
			sizedRun{"small-b", 2 * gib},
		}
		tasks := planLogMergeTasks(runs, "tenant", 4, nil)
		require.Equal(t, [][]string{{"small-a", "small-b"}, {"large"}}, taskPaths(tasks))
	})

	t.Run("splits a level into tasks of at most k runs", func(t *testing.T) {
		runs := []v2.Run{
			sizedRun{"a", 1 * gib},
			sizedRun{"b", 1 * gib},
			sizedRun{"c", 1 * gib},
		}
		tasks := planLogMergeTasks(runs, "tenant", 2, nil)
		require.Equal(t, [][]string{{"a", "b"}, {"c"}}, taskPaths(tasks))
	})

	t.Run("returns no tasks when there are no runs", func(t *testing.T) {
		require.Empty(t, planLogMergeTasks(nil, "tenant", 2, nil))
	})
}
