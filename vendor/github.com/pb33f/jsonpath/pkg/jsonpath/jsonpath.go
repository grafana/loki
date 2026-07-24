package jsonpath

import (
	"fmt"
	"github.com/pb33f/jsonpath/pkg/jsonpath/config"
	"github.com/pb33f/jsonpath/pkg/jsonpath/token"
	"go.yaml.in/yaml/v4"
)

// NewPath compiles input into a reusable JSONPath using the supplied options.
func NewPath(input string, opts ...config.Option) (*JSONPath, error) {
	cfg := config.New(opts...)
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	tokenizer := token.NewTokenizerWithConfig(input, cfg)
	tokens := tokenizer.Tokenize()
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Token == token.ILLEGAL {
			message := "unexpected token"
			if config.SpectralCompatibilityEnabled(cfg) && tokens[i].Literal != "" {
				message = "Spectral compatibility mode: " + tokens[i].Literal
			} else if !cfg.JSONPathPlusEnabled() && tokens[i].Literal != "" {
				message = tokens[i].Literal
			}
			return nil, fmt.Errorf("%s", tokenizer.ErrorString(&tokens[i], message))
		}
	}
	parser := newParserPrivateWithConfig(tokenizer, tokens, cfg)
	err := parser.parse()
	if err != nil {
		return nil, err
	}
	return parser, nil
}

// Query evaluates the compiled path against root.
func (p *JSONPath) Query(root *yaml.Node) []*yaml.Node {
	return p.ast.Query(root, root)
}

// String returns a stable normalized representation of the compiled path.
func (p *JSONPath) String() string {
	if p == nil {
		return ""
	}
	return p.ast.ToString()
}

// IsSingular reports whether the path can select at most one node.
func (p *JSONPath) IsSingular() bool {
	if p == nil {
		return false
	}
	return p.ast.isSingular()
}

// SegmentInfo describes a member-name or array-index segment in a singular path.
type SegmentInfo struct {
	Kind     SegmentKind
	Key      string
	Index    int64
	HasIndex bool
}

// SegmentKind identifies the public kind of a singular path segment.
type SegmentKind int

const (
	// SegmentKindMemberName identifies a mapping member segment.
	SegmentKindMemberName SegmentKind = iota
	// SegmentKindArrayIndex identifies a sequence index segment.
	SegmentKindArrayIndex
)

// GetSegmentInfo returns public segment metadata for a singular path.
func (p *JSONPath) GetSegmentInfo() ([]SegmentInfo, error) {
	if p == nil {
		return nil, fmt.Errorf("nil path")
	}
	return p.ast.getSegmentInfo()
}
