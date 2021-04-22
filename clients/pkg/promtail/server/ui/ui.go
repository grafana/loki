// +build dev

package ui

import (
	"net/http"
	"os"
	"strings"

	"github.com/shurcooL/httpfs/filter"
	"github.com/shurcooL/httpfs/union"
)

var static http.FileSystem = filter.Keep(
	http.Dir("./static"),
	func(path string, fi os.FileInfo) bool {
		return fi.IsDir() ||
			(!strings.HasSuffix(path, "map.js") &&
				!strings.HasSuffix(path, "/bootstrap.js") &&
				!strings.HasSuffix(path, "/bootstrap-theme.css") &&
				!strings.HasSuffix(path, "/bootstrap.css"))
	},
)

var templates http.FileSystem = filter.Keep(
	http.Dir("./templates"),
	func(path string, fi os.FileInfo) bool {
		return fi.IsDir() || strings.HasSuffix(path, ".html")
	},
)

// Assets contains the project's assets loaded from local file system when build with `-tags dev`
var Assets http.FileSystem = union.New(map[string]http.FileSystem{
	"/templates": templates,
	"/static":    static,
})
