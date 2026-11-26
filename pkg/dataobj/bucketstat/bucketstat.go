// Package bucketstat defines the xcap statistics of the object-store requests that a task sends. The
// instrumented bucket reader of [pkg/dataobj] records them.
//
// They live in a package of their own, so that the packages that pkg/dataobj imports, such as
// [pkg/dataobj/metadata], can read them.
package bucketstat

import "github.com/grafana/loki/v3/pkg/xcap"

var (
	// StatObjectBytesDownloaded is the total number of bytes a task read from
	// the backing object store. Every request made through a reader from
	// [pkg/dataobj.NewInstrumentedBucketReader] counts: metadata reads, page
	// downloads and ad-hoc reads.
	//
	// A body records its bytes when the caller closes it. Bytes served from the
	// metadata prefetch buffer are not counted again, so this is a true
	// "transferred from storage" figure.
	StatObjectBytesDownloaded = xcap.NewStatisticInt64("dataobj.object.bytes.downloaded", xcap.AggregationTypeSum)

	// StatObjectRequests* count every object-store request a task issued through
	// a reader from [pkg/dataobj.NewInstrumentedBucketReader], successful or not.
	//
	// Reads served from memory, such as the prefetch buffer, issue no request and
	// are not counted.
	StatObjectRequestsAttributes = xcap.NewStatisticInt64("dataobj.object.requests.attributes", xcap.AggregationTypeSum)
	StatObjectRequestsGet        = xcap.NewStatisticInt64("dataobj.object.requests.get", xcap.AggregationTypeSum)
	StatObjectRequestsGetRange   = xcap.NewStatisticInt64("dataobj.object.requests.get_range", xcap.AggregationTypeSum)

	// StatObjectRequestFailures* count the requests of the matching
	// StatObjectRequests* stat that failed, in the call or while reading the
	// body it returned. A not-found error or a canceled request is not a
	// failure. A request counts as failed once, even if several reads of its
	// body fail.
	StatObjectRequestFailuresAttributes = xcap.NewStatisticInt64("dataobj.object.request_failures.attributes", xcap.AggregationTypeSum)
	StatObjectRequestFailuresGet        = xcap.NewStatisticInt64("dataobj.object.request_failures.get", xcap.AggregationTypeSum)
	StatObjectRequestFailuresGetRange   = xcap.NewStatisticInt64("dataobj.object.request_failures.get_range", xcap.AggregationTypeSum)
)
