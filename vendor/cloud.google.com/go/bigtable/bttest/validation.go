/*
Copyright 2019 Google LLC

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

package bttest

import (
	"bytes"

	btpb "google.golang.org/genproto/googleapis/bigtable/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// validateRowRanges returns a status.Error for req if:
//  * both start_qualifier_closed and start_qualifier_open are set
//  * both end_qualifier_closed and end_qualifier_open are set
//  * start_qualifier_closed > end_qualifier_closed
//  * start_qualifier_closed > end_qualifier_open
//  * start_qualifier_open > end_qualifier_closed
//  * start_qualifier_open > end_qualifier_open
func validateRowRanges(req *btpb.ReadRowsRequest) error {
	rowRanges := req.GetRows().GetRowRanges()
	if len(rowRanges) == 0 {
		return nil
	}

	for i, rowRange := range rowRanges {
		skC := rowRange.GetStartKeyClosed()
		ekC := rowRange.GetEndKeyClosed()
		skO := rowRange.GetStartKeyOpen()
		ekO := rowRange.GetEndKeyOpen()

		if msg := messageOnInvalidKeyRanges(skC, skO, ekC, ekO); msg != "" {
			return status.Errorf(codes.InvalidArgument, "Error in element #%d: %s", i, msg)
		}
	}
	return nil
}

func messageOnInvalidKeyRanges(startKeyClosed, startKeyOpen, endKeyClosed, endKeyOpen []byte) string {
	switch {
	case len(startKeyClosed) != 0 && len(startKeyOpen) != 0:
		return "both start_key_closed and start_key_open cannot be set"

	case len(endKeyClosed) != 0 && len(endKeyOpen) != 0:
		return "both end_key_closed and end_key_open cannot be set"

	case keysOutOfRange(startKeyClosed, endKeyClosed):
		return "start_key_closed must be less than end_key_closed"

	case keysOutOfRange(startKeyOpen, endKeyOpen):
		return "start_key_open must be less than end_key_open"

	case keysOutOfRange(startKeyClosed, endKeyOpen):
		return "start_key_closed must be less than end_key_open"

	case keysOutOfRange(startKeyOpen, endKeyClosed):
		return "start_key_open must be less than end_key_closed"
	}
	return ""
}

func keysOutOfRange(start, end []byte) bool {
	if len(start) == 0 && len(end) == 0 {
		// Neither keys have been set, this is an implicit indefinite range.
		return false
	}
	if len(start) == 0 || len(end) == 0 {
		// Either of the keys have been set so this is an explicit indefinite range.
		return false
	}
	// Both keys have been set now check if start > end.
	return bytes.Compare(start, end) > 0
}
