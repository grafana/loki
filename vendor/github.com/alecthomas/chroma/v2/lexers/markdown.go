package lexers

import (
	"strings"

	. "github.com/alecthomas/chroma/v2" // nolint
)

// Markdown lexer with YAML frontmatter and HTML comment support.
var Markdown = Register(&markdownLexer{Lexer: MustNewLexer(
	&Config{
		Name:      "markdown",
		Aliases:   []string{"md", "mkd"},
		Filenames: []string{"*.md", "*.mkd", "*.markdown"},
		MimeTypes: []string{"text/x-markdown"},
	},
	markdownRules,
)})

// markdownLexer wraps the base Markdown lexer to highlight top-of-file YAML frontmatter.
type markdownLexer struct {
	Lexer
}

// Lexes Markdown, highlighting a leading YAML frontmatter block before delegating to Markdown rules.
func (m *markdownLexer) Tokenise(options *TokeniseOptions, text string) (Iterator, error) {
	frontmatter, rest, ok := splitFrontmatter(text)
	if !ok {
		return m.Lexer.Tokenise(options, text)
	}

	yamlLexer := Get("YAML")
	if yamlLexer == nil {
		return m.Lexer.Tokenise(options, text)
	}

	yamlTokens, err := yamlLexer.Tokenise(options, frontmatter)
	if err != nil {
		return nil, err
	}
	markdownTokens, err := m.Lexer.Tokenise(options, rest)
	if err != nil {
		return nil, err
	}
	return Concaterator(yamlTokens, markdownTokens), nil
}

// Extracts a leading YAML frontmatter block if the document starts with one.
func splitFrontmatter(text string) (frontmatter string, rest string, ok bool) {
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return "", text, false
	}

	lineEnd := strings.IndexByte(text, '\n')
	if lineEnd < 0 {
		return "", text, false
	}
	if strings.TrimSuffix(text[:lineEnd], "\r") != "---" {
		return "", text, false
	}

	for pos := lineEnd + 1; pos < len(text); {
		next := strings.IndexByte(text[pos:], '\n')
		if next < 0 {
			break
		}
		lineEnd = pos + next
		line := strings.TrimSuffix(text[pos:lineEnd], "\r")
		if line == "---" {
			return text[:lineEnd+1], text[lineEnd+1:], true
		}
		pos = lineEnd + 1
	}
	return "", text, false
}

func markdownRules() Rules {
	return Rules{
		"root": {
			{`<!--[\w\W]*?-->`, CommentMultiline, nil},
			{`^(#[^#].+\n)`, ByGroups(GenericHeading), nil},
			{`^(#{2,6}.+\n)`, ByGroups(GenericSubheading), nil},
			{`^(\s*)([*-] )(\[[ xX]\])( .+\n)`, ByGroups(Text, Keyword, Keyword, UsingSelf("inline")), nil},
			{`^(\s*)([*-])(\s)(.+\n)`, ByGroups(Text, Keyword, Text, UsingSelf("inline")), nil},
			{`^(\s*)([0-9]+\.)( .+\n)`, ByGroups(Text, Keyword, UsingSelf("inline")), nil},
			{`^(\s*>\s)(.+\n)`, ByGroups(Keyword, GenericEmph), nil},
			{"^(```\\n)([\\w\\W]*?)(^```$)", ByGroups(String, Text, String), nil},
			{
				"^(```)(\\w+)(\\n)([\\w\\W]*?)(^```$)",
				UsingByGroup(2, 4, String, String, String, Text, String),
				nil,
			},
			Include("inline"),
		},
		"inline": {
			{`<!--[\w\W]*?-->`, CommentMultiline, nil},
			{`\\.`, Text, nil},
			{`(\s)(\*|_)((?:(?!\2).)*)(\2)((?=\W|\n))`, ByGroups(Text, GenericEmph, GenericEmph, GenericEmph, Text), nil},
			{`(\s)((\*\*|__).*?)\3((?=\W|\n))`, ByGroups(Text, GenericStrong, GenericStrong, Text), nil},
			{`(\s)(~~[^~]+~~)((?=\W|\n))`, ByGroups(Text, GenericDeleted, Text), nil},
			{"`[^`]+`", LiteralStringBacktick, nil},
			{`[@#][\w/:]+`, NameEntity, nil},
			{`(!?\[)([^]]+)(\])(\()([^)]+)(\))`, ByGroups(Text, NameTag, Text, Text, NameAttribute, Text), nil},
			{`.|\n`, Text, nil},
		},
	}
}
