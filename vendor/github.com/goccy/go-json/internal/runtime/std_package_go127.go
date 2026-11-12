//go:build go1.27

package runtime

func init() {
	// uuid is of Go 1.27.
	stdMarshalerPackages["uuid"] = true
}
