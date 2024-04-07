// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/tools/doc-generator/writer.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/regexp"
	"github.com/mitchellh/go-wordwrap"
	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/tools/doc-generator/parse"
)

type specWriter struct {
	out strings.Builder
}

func (w *specWriter) writeConfigBlock(b *parse.ConfigBlock, indent int) {
	if len(b.Entries) == 0 {
		return
	}

	var written bool
	for i, entry := range b.Entries {
		// Add a new line to separate from the previous entry
		if written && i > 0 {
			w.out.WriteString("\n")
		}

		written = w.writeConfigEntry(entry, indent)
	}
}

// nolint:goconst
func (w *specWriter) writeConfigEntry(e *parse.ConfigEntry, indent int) (written bool) {
	written = true
	if e.Kind == parse.KindBlock {
		// If the block is a root block it will have its dedicated section in the doc,
		// so here we've just to write down the reference without re-iterating on it.
		if e.Root {
			// Description
			w.writeComment(e.BlockDesc, indent, 0)
			if e.Block.FlagsPrefix != "" {
				w.writeComment(fmt.Sprintf("The CLI flags prefix for this block configuration is: %s", e.Block.FlagsPrefix), indent, 0)
			}

			// Block reference without entries, because it's a root block
			w.out.WriteString(pad(indent) + "[" + e.Name + ": <" + e.Block.Name + ">]\n")
		} else {
			// Description
			w.writeComment(e.BlockDesc, indent, 0)

			// Name
			w.out.WriteString(pad(indent) + e.Name + ":\n")

			// Entries
			w.writeConfigBlock(e.Block, indent+tabWidth)
		}
	}

	if e.Kind == parse.KindField || e.Kind == parse.KindSlice || e.Kind == parse.KindMap {
		if strings.HasPrefix(e.Description(), "IGNORED:") {
			// We skip documenting any field whose description starts with "IGNORED:".
			return false
		}

		// Description
		w.writeComment(e.Description(), indent, 0)
		w.writeExample(e.FieldExample, indent)
		w.writeFlag(e.FieldFlag, indent)

		// Specification
		fieldDefault := e.FieldDefault
		if e.FieldType == "string" {
			fieldDefault = strconv.Quote(fieldDefault)
		} else if e.FieldType == "duration" {
			fieldDefault = cleanupDuration(fieldDefault)
		}

		if e.Required {
			w.out.WriteString(pad(indent) + e.Name + ": <" + e.FieldType + "> | default = " + fieldDefault + "\n")
		} else {
			defaultValue := ""
			if len(fieldDefault) > 0 {
				defaultValue = " | default = " + fieldDefault
			}
			w.out.WriteString(pad(indent) + "[" + e.Name + ": <" + e.FieldType + ">" + defaultValue + "]\n")
		}
	}

	return written
}

func (w *specWriter) writeFlag(name string, indent int) {
	if name == "" {
		return
	}

	w.out.WriteString(pad(indent) + "# CLI flag: -" + name + "\n")
}

func (w *specWriter) writeComment(comment string, indent, innerIndent int) {
	if comment == "" {
		return
	}

	wrapped := wordwrap.WrapString(comment, uint(maxLineWidth-indent-innerIndent-2))
	w.writeWrappedString(wrapped, indent, innerIndent)
}

func (w *specWriter) writeExample(example *parse.FieldExample, indent int) {
	if example == nil {
		return
	}

	w.writeComment("Example:", indent, 0)
	if example.Comment != "" {
		w.writeComment(example.Comment, indent, 2)
	}

	data, err := yaml.Marshal(example.Yaml)
	if err != nil {
		panic(fmt.Errorf("can't render example: %w", err))
	}

	w.writeWrappedString(string(data), indent, 2)
}

func (w *specWriter) writeWrappedString(s string, indent, innerIndent int) {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for _, line := range lines {
		w.out.WriteString(pad(indent) + "# " + pad(innerIndent) + line + "\n")
	}
}

func (w *specWriter) string() string {
	return strings.TrimSpace(w.out.String())
}

type markdownWriter struct {
	out strings.Builder
}

func (w *markdownWriter) writeConfigDoc(blocks []*parse.ConfigBlock) {
	// Deduplicate root blocks.
	uniqueBlocks := map[string]*parse.ConfigBlock{}
	for _, block := range blocks {
		uniqueBlocks[block.Name] = block
	}

	// Generate the markdown, honoring the root blocks order.
	if topBlock, ok := uniqueBlocks[""]; ok {
		w.writeConfigBlock(topBlock)
	}

	for _, rootBlock := range parse.RootBlocks {
		if block, ok := uniqueBlocks[rootBlock.Name]; ok {
			// Keep the root block description.
			blockToWrite := *block
			blockToWrite.Desc = rootBlock.Desc

			w.writeConfigBlock(&blockToWrite)
		}
	}
}

func (w *markdownWriter) writeConfigBlock(block *parse.ConfigBlock) {
	// Title
	if block.Name != "" {
		w.out.WriteString("### " + block.Name + "\n")
		w.out.WriteString("\n")
	}

	// Description
	if block.Desc != "" {
		desc := block.Desc

		// Wrap first instance of the config block name with backticks
		if block.Name != "" {
			var matches int
			nameRegexp := regexp.MustCompile(regexp.QuoteMeta(block.Name))
			desc = nameRegexp.ReplaceAllStringFunc(desc, func(input string) string {
				if matches == 0 {
					matches++
					return "`" + input + "`"
				}
				return input
			})
		}

		// List of all prefixes used to reference this config block.
		if len(block.FlagsPrefixes) > 1 {
			sortedPrefixes := sort.StringSlice(block.FlagsPrefixes)
			sortedPrefixes.Sort()

			desc += " The supported CLI flags `<prefix>` used to reference this configuration block are:\n\n"

			for _, prefix := range sortedPrefixes {
				if prefix == "" {
					desc += "- _no prefix_\n"
				} else {
					desc += fmt.Sprintf("- `%s`\n", prefix)
				}
			}

			// Unfortunately the markdown compiler used by the website generator has a bug
			// when there's a list followed by a code block (no matter know many newlines
			// in between). To workaround, we add a non-breaking space.
			desc += "\n&nbsp;"
		}

		w.out.WriteString(desc + "\n")
		w.out.WriteString("\n")
	}

	// Config specs
	spec := &specWriter{}
	spec.writeConfigBlock(block, 0)

	w.out.WriteString("```yaml\n")
	w.out.WriteString(spec.string() + "\n")
	w.out.WriteString("```\n")
	w.out.WriteString("\n")
}

func (w *markdownWriter) string() string {
	return strings.TrimSpace(w.out.String())
}

func pad(length int) string {
	return strings.Repeat(" ", length)
}

func cleanupDuration(value string) string {
	// This is the list of suffixes to remove from the duration if they're not
	// the whole duration value.
	suffixes := []string{"0s", "0m"}

	for _, suffix := range suffixes {
		re := regexp.MustCompile("(^.+\\D)" + suffix + "$")

		if groups := re.FindStringSubmatch(value); len(groups) == 2 {
			value = groups[1]
		}
	}

	return value
}
