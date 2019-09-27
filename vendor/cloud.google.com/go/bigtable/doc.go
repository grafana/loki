/*
Copyright 2015 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Package bigtable is an API to Google Cloud Bigtable.

See https://cloud.google.com/bigtable/docs/ for general product documentation.

See https://godoc.org/cloud.google.com/go for authentication, timeouts,
connection pooling and similar aspects of this package.


Reading

The principal way to read from a Bigtable is to use the ReadRows method on
*Table. A RowRange specifies a contiguous portion of a table. A Filter may be
provided through RowFilter to limit or transform the data that is returned.

	tbl := client.Open("mytable")
	// Read all the rows starting with "com.google.", but only fetch the columns
	// in the "links" family.
	rr := bigtable.PrefixRange("com.google.")
	err := tbl.ReadRows(ctx, rr, func(r Row) bool {
		// TODO: do something with r.
		return true // Keep going.
	}, bigtable.RowFilter(bigtable.FamilyFilter("links")))
	if err != nil {
		// TODO: handle err.
	}

To read a single row, use the ReadRow helper method:

	r, err := tbl.ReadRow(ctx, "com.google.cloud") // "com.google.cloud" is the entire row key
	if err != nil {
		// TODO: handle err.
	}
	// TODO: use r.


Writing

This API exposes two distinct forms of writing to a Bigtable: a Mutation and a
ReadModifyWrite. The former expresses idempotent operations. The latter
expresses non-idempotent operations and returns the new values of updated cells.
These operations are performed by creating a Mutation or ReadModifyWrite (with
NewMutation or NewReadModifyWrite), building up one or more operations on that,
and then using the Apply or ApplyReadModifyWrite methods on a Table.

For instance, to set a couple of cells in a table:

	tbl := client.Open("mytable")
	mut := bigtable.NewMutation()
	// To use numeric values that will later be incremented,
	// they need to be big-endian encoded as 64-bit integers.
	buf := new(bytes.Buffer)
	initialLinkCount := 1 // The initial number of links.
	if err := binary.Write(buf, binary.BigEndian, initialLinkCount); err != nil {
	// TODO: handle err.
	}
	mut.Set("links", "maps.google.com", bigtable.Now(), buf.Bytes())
	mut.Set("links", "golang.org", bigtable.Now(), buf.Bytes())
	err := tbl.Apply(ctx, "com.google.cloud", mut)
	if err != nil {
		// TODO: handle err.
	}

To increment an encoded value in one cell:

	tbl := client.Open("mytable")
	rmw := bigtable.NewReadModifyWrite()
	rmw.Increment("links", "golang.org", 12) // add 12 to the cell in column "links:golang.org"
	r, err := tbl.ApplyReadModifyWrite(ctx, "com.google.cloud", rmw)
	if err != nil {
		// TODO: handle err.
	}
	// TODO: use r.


Retries

If a read or write operation encounters a transient error it will be retried
until a successful response, an unretryable error or the context deadline is
reached. Non-idempotent writes (where the timestamp is set to ServerTime) will
not be retried. In the case of ReadRows, retried calls will not re-scan rows
that have already been processed.
*/
package bigtable // import "cloud.google.com/go/bigtable"

// Scope constants for authentication credentials. These should be used when
// using credential creation functions such as oauth.NewServiceAccountFromFile.
const (
	// Scope is the OAuth scope for Cloud Bigtable data operations.
	Scope = "https://www.googleapis.com/auth/bigtable.data"
	// ReadonlyScope is the OAuth scope for Cloud Bigtable read-only data
	// operations.
	ReadonlyScope = "https://www.googleapis.com/auth/bigtable.readonly"

	// AdminScope is the OAuth scope for Cloud Bigtable table admin operations.
	AdminScope = "https://www.googleapis.com/auth/bigtable.admin.table"

	// InstanceAdminScope is the OAuth scope for Cloud Bigtable instance (and
	// cluster) admin operations.
	InstanceAdminScope = "https://www.googleapis.com/auth/bigtable.admin.cluster"
)

// clientUserAgent identifies the version of this package.
// It should be bumped upon significant changes only.
const clientUserAgent = "cbt-go/20180601"

// resourcePrefixHeader is the name of the metadata header used to indicate
// the resource being operated on.
const resourcePrefixHeader = "google-cloud-resource-prefix"
