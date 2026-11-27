package physical

// TaskName returns the name of the compaction task that node is the root of:
// index-merge, log-merge, index-filter, or deletion-sweep. Logs, metric
// labels, and scheduler actors use the name, so a change to a name breaks
// dashboards and queues.
//
// TaskName returns false if node has no task name.
func TaskName(node Node) (string, bool) {
	switch node.(type) {
	case *IndexMerge:
		return "index-merge", true
	case *LogMerge:
		return "log-merge", true
	case *IndexFilter:
		return "index-filter", true
	case *DeletionSweep:
		return "deletion-sweep", true
	default:
		return "", false
	}
}
