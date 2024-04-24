package styles

import (
	"github.com/alecthomas/chroma"
)

// AlgolNu style.
var AlgolNu = Register(chroma.MustNewStyle("algol_nu", chroma.StyleEntries{
	chroma.Comment:            "italic #888",
	chroma.CommentPreproc:     "bold noitalic #888",
	chroma.CommentSpecial:     "bold noitalic #888",
	chroma.Keyword:            "bold",
	chroma.KeywordDeclaration: "italic",
	chroma.NameBuiltin:        "bold italic",
	chroma.NameBuiltinPseudo:  "bold italic",
	chroma.NameNamespace:      "bold italic #666",
	chroma.NameClass:          "bold italic #666",
	chroma.NameFunction:       "bold italic #666",
	chroma.NameVariable:       "bold italic #666",
	chroma.NameConstant:       "bold italic #666",
	chroma.OperatorWord:       "bold",
	chroma.LiteralString:      "italic #666",
	chroma.Error:              "border:#FF0000",
	chroma.Background:         " bg:#ffffff",
}))
