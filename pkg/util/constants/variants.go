package constants

// Re-export from client module to maintain code reuse
import "github.com/grafana/loki/client/util"

// VariantLabel is the name of the label used to identify which variant a series belongs to
// in multi-variant queries.
const VariantLabel = util.VariantLabel
