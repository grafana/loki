package util

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"unsafe"

	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
)

const maxStackSize = 8 * 1024
const sep = "\xff"

func BuildIndexFileName(tableName, uploader, dbName string) string {
	// Files are stored with <uploader>-<db-name>
	objectKey := fmt.Sprintf("%s-%s", uploader, dbName)

	// if the file is a migrated one then don't add its name to the object key otherwise we would re-upload them again here with a different name.
	if tableName == dbName {
		objectKey = uploader
	}

	return objectKey
}

type result struct {
	boltdb *bbolt.DB
	err    error
}

// SafeOpenBoltdbFile will recover from a panic opening a DB file, and return the panic message in the err return object.
func SafeOpenBoltdbFile(path string) (*bbolt.DB, error) {
	result := make(chan *result)
	// Open the file in a separate goroutine because we want to change
	// the behavior of a Fault for just this operation and not for the
	// calling goroutine
	go safeOpenBoltDbFile(path, result)
	res := <-result
	return res.boltdb, res.err
}

func safeOpenBoltDbFile(path string, ret chan *result) {
	// boltdb can throw faults which are not caught by recover unless we turn them into panics
	debug.SetPanicOnFault(true)
	res := &result{}

	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
			res.err = fmt.Errorf("recovered from panic opening boltdb file: %v", r)
		}

		// Return the result object on the channel to unblock the calling thread
		ret <- res
	}()

	b, err := local.OpenBoltdbFile(path)
	res.boltdb = b
	res.err = err
}

// func QueryKey(q index.Query) string {
// 	ret := q.TableName + sep + q.HashValue

// 	if len(q.RangeValuePrefix) != 0 {
// 		ret += sep + string(q.RangeValuePrefix)
// 	}

// 	if len(q.RangeValueStart) != 0 {
// 		ret += sep + string(q.RangeValueStart)
// 	}

// 	if len(q.ValueEqual) != 0 {
// 		ret += sep + string(q.ValueEqual)
// 	}

// 	return ret
// }

func GetUnsafeBytes(s string) []byte {
	return *((*[]byte)(unsafe.Pointer(&s))) // #nosec G103 -- we know the string is not mutated
}

func GetUnsafeString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf))) // #nosec G103 -- we know the string is not mutated
}

func logPanic(p interface{}) {
	stack := make([]byte, maxStackSize)
	stack = stack[:runtime.Stack(stack, true)]
	// keep a multiline stack
	fmt.Fprintf(os.Stderr, "panic: %v\n%s", p, stack)
}
