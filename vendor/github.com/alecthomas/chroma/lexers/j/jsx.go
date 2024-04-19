package j

import (
	. "github.com/alecthomas/chroma" // nolint
	"github.com/alecthomas/chroma/lexers/internal"
)

// JSX lexer.
//
// This was generated from https://github.com/fcurella/jsx-lexer
var JSX = internal.Register(MustNewLazyLexer(
	&Config{
		Name:      "react",
		Aliases:   []string{"jsx", "react"},
		Filenames: []string{"*.jsx", "*.react"},
		MimeTypes: []string{"text/jsx", "text/typescript-jsx"},
		DotAll:    true,
	},
	jsxRules,
))

func jsxRules() Rules {
	return Rules{
		"commentsandwhitespace": {
			{`\s+`, Text, nil},
			{`<!--`, Comment, nil},
			{`//.*?\n`, CommentSingle, nil},
			{`/\*.*?\*/`, CommentMultiline, nil},
		},
		"slashstartsregex": {
			Include("commentsandwhitespace"),
			{`/(\\.|[^[/\\\n]|\[(\\.|[^\]\\\n])*])+/([gimuy]+\b|\B)`, LiteralStringRegex, Pop(1)},
			{`(?=/)`, Text, Push("#pop", "badregex")},
			Default(Pop(1)),
		},
		"badregex": {
			{`\n`, Text, Pop(1)},
		},
		"root": {
			Include("jsx"),
			{`\A#! ?/.*?\n`, CommentHashbang, nil},
			{`^(?=\s|/|<!--)`, Text, Push("slashstartsregex")},
			Include("commentsandwhitespace"),
			{`(\.\d+|[0-9]+\.[0-9]*)([eE][-+]?[0-9]+)?`, LiteralNumberFloat, nil},
			{`0[bB][01]+`, LiteralNumberBin, nil},
			{`0[oO][0-7]+`, LiteralNumberOct, nil},
			{`0[xX][0-9a-fA-F]+`, LiteralNumberHex, nil},
			{`[0-9]+`, LiteralNumberInteger, nil},
			{`\.\.\.|=>`, Punctuation, nil},
			{`\+\+|--|~|&&|\?|:|\|\||\\(?=\n)|(<<|>>>?|==?|!=?|[-<>+*%&|^/])=?`, Operator, Push("slashstartsregex")},
			{`[{(\[;,]`, Punctuation, Push("slashstartsregex")},
			{`[})\].]`, Punctuation, nil},
			{`(for|in|while|do|break|return|continue|switch|case|default|if|else|throw|try|catch|finally|new|delete|typeof|instanceof|void|yield|this|of)\b`, Keyword, Push("slashstartsregex")},
			{`(var|let|with|function)\b`, KeywordDeclaration, Push("slashstartsregex")},
			{`(abstract|async|await|boolean|byte|char|class|const|debugger|double|enum|export|extends|final|float|goto|implements|import|int|interface|long|native|package|private|protected|public|short|static|super|synchronized|throws|transient|volatile)\b`, KeywordReserved, nil},
			{`(true|false|null|NaN|Infinity|undefined)\b`, KeywordConstant, nil},
			{`(Array|Boolean|Date|Error|Function|Math|netscape|Number|Object|Packages|RegExp|String|Promise|Proxy|sun|decodeURI|decodeURIComponent|encodeURI|encodeURIComponent|Error|eval|isFinite|isNaN|isSafeInteger|parseFloat|parseInt|document|this|window)\b`, NameBuiltin, nil},
			{`(?:[$_\p{L}\p{N}]|\\u[a-fA-F0-9]{4})(?:(?:[$\p{L}\p{N}]|\\u[a-fA-F0-9]{4}))*`, NameOther, nil},
			{`"(\\\\|\\"|[^"])*"`, LiteralStringDouble, nil},
			{`'(\\\\|\\'|[^'])*'`, LiteralStringSingle, nil},
			{"`", LiteralStringBacktick, Push("interp")},
		},
		"interp": {
			{"`", LiteralStringBacktick, Pop(1)},
			{`\\\\`, LiteralStringBacktick, nil},
			{"\\\\`", LiteralStringBacktick, nil},
			{`\$\{`, LiteralStringInterpol, Push("interp-inside")},
			{`\$`, LiteralStringBacktick, nil},
			{"[^`\\\\$]+", LiteralStringBacktick, nil},
		},
		"interp-inside": {
			{`\}`, LiteralStringInterpol, Pop(1)},
			Include("root"),
		},
		"jsx": {
			{`(<)(/?)(>)`, ByGroups(Punctuation, Punctuation, Punctuation), nil},
			{`(<)([\w\.]+)`, ByGroups(Punctuation, NameTag), Push("tag")},
			{`(<)(/)([\w\.]+)(>)`, ByGroups(Punctuation, Punctuation, NameTag, Punctuation), nil},
		},
		"tag": {
			{`\s+`, Text, nil},
			{`([\w]+\s*)(=)(\s*)`, ByGroups(NameAttribute, Operator, Text), Push("attr")},
			{`[{}]+`, Punctuation, nil},
			{`[\w\.]+`, NameAttribute, nil},
			{`(/?)(\s*)(>)`, ByGroups(Punctuation, Text, Punctuation), Pop(1)},
		},
		"attr": {
			{`{`, Punctuation, Push("expression")},
			{`".*?"`, LiteralString, Pop(1)},
			{`'.*?'`, LiteralString, Pop(1)},
			Default(Pop(1)),
		},
		"expression": {
			{`{`, Punctuation, Push()},
			{`}`, Punctuation, Pop(1)},
			Include("root"),
		},
	}
}
