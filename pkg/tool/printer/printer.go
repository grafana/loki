package printer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/alecthomas/chroma/quick"
	"github.com/mitchellh/colorstring"
	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/pkg/tool/rules"
	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

// Printer is  used for printing formatted output from the lokitool
type Printer struct {
	disableColor bool
	colorizer    colorstring.Colorize
}

// New returns a Printer struct
func New(color bool) *Printer {
	return &Printer{
		disableColor: color,
		colorizer: colorstring.Colorize{
			Colors:  colorstring.DefaultColors,
			Reset:   true,
			Disable: color,
		},
	}
}

// Println is a convenience wrapper for fmt.Println with support for color
// codes.
func (p *Printer) Println(a string) {
	fmt.Println(p.colorizer.Color(a))
}

// Printf is a convenience wrapper for fmt.Printf with support for color
// codes.
func (p *Printer) Printf(format string, a ...interface{}) {
	fmt.Printf(p.colorizer.Color(format), a...)
}

// PrintAlertmanagerConfig prints the current alertmanager config
func (p *Printer) PrintAlertmanagerConfig(config string, templates map[string]string) error {

	// go-text-template
	if !p.disableColor {
		err := quick.Highlight(os.Stdout, config, "yaml", "terminal", "swapoff")
		if err != nil {
			return err
		}
	} else {
		fmt.Println(config)
	}

	fmt.Printf("\nTemplates:\n")
	for fn, template := range templates {
		fmt.Println(fn + ":")
		if !p.disableColor {
			err := quick.Highlight(os.Stdout, template, "go-text-template", "terminal", "swapoff")
			if err != nil {
				return nil
			}
		} else {
			fmt.Println(template)
		}
	}

	return nil
}

// PrintRuleGroups prints the current alertmanager config
func (p *Printer) PrintRuleGroups(rules map[string][]rwrulefmt.RuleGroup) error {
	encodedRules, err := yaml.Marshal(&rules)
	if err != nil {
		return err
	}

	// go-text-template
	if !p.disableColor {
		return quick.Highlight(os.Stdout, string(encodedRules), "yaml", "terminal", "swapoff")
	}

	fmt.Println(string(encodedRules))

	return nil
}

// PrintRuleGroup prints the current alertmanager config
func (p *Printer) PrintRuleGroup(rule rwrulefmt.RuleGroup) error {
	encodedRule, err := yaml.Marshal(&rule)
	if err != nil {
		return err
	}

	// go-text-template
	if !p.disableColor {
		return quick.Highlight(os.Stdout, string(encodedRule), "yaml", "terminal", "swapoff")
	}

	fmt.Println(string(encodedRule))

	return nil
}

// PrintComparisonResult prints the differences between the staged rules namespace
// and active rules namespace
func (p *Printer) PrintComparisonResult(results []rules.NamespaceChange, verbose bool) error {
	created, updated, deleted := rules.SummarizeChanges(results)

	// If any changes are detected, print the symbol legend
	if (created + updated + deleted) > 0 {
		fmt.Println("Changes are indicated with the following symbols:")
		if created > 0 {
			p.Println("[green]  +[reset] created")
		}
		if updated > 0 {
			p.Println("[yellow]  ~[reset] updated")
		}
		if deleted > 0 {
			p.Println("[red]  -[reset] deleted")
		}
		fmt.Println()
		fmt.Println("The following changes will be made if the provided rule set is synced:")
	} else {
		fmt.Println("no changes detected")
		return nil
	}

	for _, change := range results {
		switch change.State {
		case rules.Created:
			p.Printf("[green]+ Namespace: %v\n", change.Namespace)
			for _, c := range change.GroupsCreated {
				p.Printf("[green]  + Group: %v\n", c.Name)
			}
		case rules.Updated:
			p.Printf("[yellow]~ Namespace: %v\n", change.Namespace)
			for _, c := range change.GroupsCreated {
				p.Printf("[green]  + Group: %v\n", c.Name)
			}

			for _, c := range change.GroupsUpdated {
				p.Printf("[yellow]  ~ Group: %v\n", c.New.Name)

				// Print the full diff of the rules if verbose is set
				if verbose {
					newYaml, _ := yaml.Marshal(c.New)
					separated := strings.Split(string(newYaml), "\n")
					for _, l := range separated {
						p.Printf("[green]+ %v\n", l)
					}

					oldYaml, _ := yaml.Marshal(c.Original)
					separated = strings.Split(string(oldYaml), "\n")
					for _, l := range separated {
						p.Printf("[red]- %v\n", l)
					}
				}
			}

			for _, c := range change.GroupsDeleted {
				p.Printf("[red]  - Group: %v\n", c.Name)
			}
		case rules.Deleted:
			p.Printf("[red]- Namespace: %v\n", change.Namespace)
			for _, c := range change.GroupsDeleted {
				p.Printf("[red]  - Group: %v\n", c.Name)
			}
		}
	}

	fmt.Println()
	fmt.Printf("Diff Summary: %v Groups Created, %v Groups Updated, %v Groups Deleted\n", created, updated, deleted)
	return nil
}

func (p *Printer) PrintRuleSet(rules map[string][]rwrulefmt.RuleGroup, format string, writer io.Writer) error {
	nsKeys := make([]string, 0, len(rules))
	for k := range rules {
		nsKeys = append(nsKeys, k)
	}
	sort.Strings(nsKeys)

	type namespaceAndRuleGroup struct {
		Namespace string `json:"namespace" yaml:"namespace"`
		RuleGroup string `json:"rulegroup" yaml:"rulegroup"`
	}
	var items []namespaceAndRuleGroup

	for _, ns := range nsKeys {
		for _, rg := range rules[ns] {
			items = append(items, namespaceAndRuleGroup{
				Namespace: ns,
				RuleGroup: rg.Name,
			})
		}
	}

	switch format {
	case "json":
		output, err := json.Marshal(items)
		if err != nil {
			return err
		}

		// go-text-template
		if !p.disableColor {
			return quick.Highlight(writer, string(output), "json", "terminal", "swapoff")
		}

		fmt.Fprint(writer, string(output))
	case "yaml":
		output, err := yaml.Marshal(items)
		if err != nil {
			return err
		}

		// go-text-template
		if !p.disableColor {
			return quick.Highlight(writer, string(output), "yaml", "terminal", "swapoff")
		}

		fmt.Fprint(writer, string(output))
	default:
		w := tabwriter.NewWriter(writer, 0, 0, 1, ' ', tabwriter.Debug)

		fmt.Fprintln(w, "Namespace\t Rule Group")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t %s\n", item.Namespace, item.RuleGroup)
		}

		w.Flush()
	}

	return nil
}
