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
	http.Dir("./pkg/promtail/server/ui/static"),
	func(path string, fi os.FileInfo) bool {
		return fi.IsDir() ||
			(!strings.HasSuffix(path, "map.js") &&
				!strings.HasSuffix(path, "/bootstrap.js") &&
				!strings.HasSuffix(path, "/bootstrap-theme.css") &&
				!strings.HasSuffix(path, "/bootstrap.css"))
	},
)

var templates http.FileSystem = filter.Keep(
	http.Dir("./pkg/promtail/server/ui/templates"),
	func(path string, fi os.FileInfo) bool {
		return fi.IsDir() || strings.HasSuffix(path, ".html")
	},
)

// Assets contains the project's assets.
var Assets http.FileSystem = union.New(map[string]http.FileSystem{
	"/templates": templates,
	"/static":    static,
})
