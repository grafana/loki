package styles

import (
	"embed"
	"io/fs"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

//go:embed *.xml
var embedded embed.FS

// Registry of Styles.
var Registry = func() map[string]*chroma.Style {
	registry := map[string]*chroma.Style{}
	// Register all embedded styles.
	files, err := fs.ReadDir(embedded, ".")
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		r, err := embedded.Open(file.Name())
		if err != nil {
			panic(err)
		}
		style, err := chroma.NewXMLStyle(r)
		if err != nil {
			panic(err)
		}
		registry[strings.ToLower(style.Name)] = style
		_ = r.Close()
	}
	return registry
}()

// Fallback style. Reassign to change the default fallback style.
var Fallback = Registry["swapoff"]

// Register a chroma.Style.
func Register(style *chroma.Style) *chroma.Style {
	Registry[strings.ToLower(style.Name)] = style
	return style
}

// Names of all available styles.
func Names() []string {
	out := []string{}
	for name := range Registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Get named style, or Fallback.
func Get(name string) *chroma.Style {
	if style, ok := Registry[strings.ToLower(name)]; ok {
		return style
	}
	return Fallback
}

// GetForMode returns the named style if it already matches mode, otherwise its
// registered counterpart if one exists and matches mode. If neither matches,
// the originally-requested style is returned (or Fallback if the name is
// unknown), so callers always get something usable.
func GetForMode(name string, mode chroma.Mode) *chroma.Style {
	style := Get(name)
	if style.Mode() == mode {
		return style
	}
	if style.Counterpart == "" {
		return style
	}
	counterpart, ok := Registry[style.Counterpart]
	if !ok || counterpart.Mode() != mode {
		return style
	}
	return counterpart
}

// RegisterPair links two styles as light/dark counterparts of each other.
//
// Both styles are also registered if they are not already present.
func RegisterPair(a, b *chroma.Style) {
	Register(a)
	Register(b)
	a.Counterpart = strings.ToLower(b.Name)
	b.Counterpart = strings.ToLower(a.Name)
}
