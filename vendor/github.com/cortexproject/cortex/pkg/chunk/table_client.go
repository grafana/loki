package chunk

import "context"

// TableClient is a client for telling Dynamo what to do with tables.
type TableClient interface {
	ListTables(ctx context.Context) ([]string, error)
	CreateTable(ctx context.Context, desc TableDesc) error
	DescribeTable(ctx context.Context, name string) (desc TableDesc, isActive bool, err error)
	UpdateTable(ctx context.Context, current, expected TableDesc) error
}

// TableDesc describes a table.
type TableDesc struct {
	Name             string
	ProvisionedRead  int64
	ProvisionedWrite int64
	Tags             Tags
	WriteScale       AutoScalingConfig
}

// Equals returns true if other matches desc.
func (desc TableDesc) Equals(other TableDesc) bool {
	if desc.WriteScale != other.WriteScale {
		return false
	}

	if desc.ProvisionedRead != other.ProvisionedRead {
		return false
	}

	// Only check provisioned write if auto scaling is disabled
	if !desc.WriteScale.Enabled && desc.ProvisionedWrite != other.ProvisionedWrite {
		return false
	}

	if !desc.Tags.Equals(other.Tags) {
		return false
	}

	return true
}

type byName []TableDesc

func (a byName) Len() int           { return len(a) }
func (a byName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byName) Less(i, j int) bool { return a[i].Name < a[j].Name }
