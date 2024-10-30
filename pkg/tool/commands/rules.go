package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/rulefmt"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
	yamlv3 "gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/pkg/tool/client"
	"github.com/grafana/loki/v3/pkg/tool/printer"
	"github.com/grafana/loki/v3/pkg/tool/rules"
	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

const (
	defaultPrepareAggregationLabel = "cluster"
)

var (
	ruleLoadTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "last_rule_load_timestamp_seconds",
		Help:      "The timestamp of the last rule load.",
	})
	ruleLoadSuccessTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "last_rule_load_success_timestamp_seconds",
		Help:      "The timestamp of the last successful rule load.",
	})

	formats = []string{"json", "yaml", "table"} // list of supported formats for the list command
)

// RuleCommand configures and executes rule related Loki operations
type RuleCommand struct {
	ClientConfig client.Config

	cli *client.LokiClient

	// Backend type (loki only)
	Backend string

	// Get Rule Groups Configs
	Namespace string
	RuleGroup string

	// Load Rules Config
	RuleFilesList []string
	RuleFiles     string
	RuleFilesPath string

	// Sync/Diff Rules Config
	Namespaces           string
	namespacesMap        map[string]struct{}
	IgnoredNamespaces    string
	ignoredNamespacesMap map[string]struct{}

	// Prepare Rules Config
	InPlaceEdit                            bool
	AggregationLabel                       string
	AggregationLabelExcludedRuleGroups     string
	aggregationLabelExcludedRuleGroupsList map[string]struct{}

	// Lint Rules Config
	LintDryRun bool

	// Rules check flags
	Strict bool

	// List Rules Config
	Format string

	DisableColor bool

	// Diff Rules Config
	Verbose bool
}

// Register rule related commands and flags with the kingpin application
func (r *RuleCommand) Register(app *kingpin.Application) {
	rulesCmd := app.Command("rules", "View & edit rules stored in loki.").PreAction(r.setup)
	rulesCmd.Flag("authToken", "Authentication token for bearer token or JWT auth, alternatively set LOKI_AUTH_TOKEN.").Default("").Envar("LOKI_AUTH_TOKEN").StringVar(&r.ClientConfig.AuthToken)
	rulesCmd.Flag("user", "API user to use when contacting loki, alternatively set LOKI_API_USER. If empty, LOKI_TENANT_ID will be used instead.").Default("").Envar("LOKI_API_USER").StringVar(&r.ClientConfig.User)
	rulesCmd.Flag("key", "API key to use when contacting loki, alternatively set LOKI_API_KEY.").Default("").Envar("LOKI_API_KEY").StringVar(&r.ClientConfig.Key)

	// Register rule commands
	listCmd := rulesCmd.
		Command("list", "List the rules currently in the loki ruler.").
		Action(r.listRules)
	printRulesCmd := rulesCmd.
		Command("print", "Print the rules currently in the loki ruler.").
		Action(r.printRules)
	getRuleGroupCmd := rulesCmd.
		Command("get", "Retrieve a rulegroup from the ruler.").
		Action(r.getRuleGroup)
	deleteRuleGroupCmd := rulesCmd.
		Command("delete", "Delete a rulegroup from the ruler.").
		Action(r.deleteRuleGroup)
	loadRulesCmd := rulesCmd.
		Command("load", "load a set of rules to a designated loki endpoint").
		Action(r.loadRules)
	diffRulesCmd := rulesCmd.
		Command("diff", "diff a set of rules to a designated loki endpoint").
		Action(r.diffRules)
	syncRulesCmd := rulesCmd.
		Command("sync", "sync a set of rules to a designated loki endpoint").
		Action(r.syncRules)
	prepareCmd := rulesCmd.
		Command("prepare", "modifies a set of rules by including an specific label in aggregations.").
		Action(r.prepare)
	lintCmd := rulesCmd.
		Command("lint", "formats a set of rule files. It reorders keys alphabetically, uses 4 spaces as indentantion, and formats PromQL expressions to a single line.").
		Action(r.lint)
	checkCmd := rulesCmd.
		Command("check", "runs various best practice checks against rules.").
		Action(r.checkRecordingRuleNames)

	// Require Loki cluster address and tentant ID on all these commands
	for _, c := range []*kingpin.CmdClause{listCmd, printRulesCmd, getRuleGroupCmd, deleteRuleGroupCmd, loadRulesCmd, diffRulesCmd, syncRulesCmd} {
		c.Flag("address", "Address of the loki cluster, alternatively set LOKI_ADDRESS.").
			Envar("LOKI_ADDRESS").
			Required().
			StringVar(&r.ClientConfig.Address)

		c.Flag("id", "Loki tenant id, alternatively set LOKI_TENANT_ID.").
			Envar("LOKI_TENANT_ID").
			Required().
			StringVar(&r.ClientConfig.ID)

		c.Flag("use-legacy-routes", "If set, API requests to loki will use the legacy /api/prom/ routes, alternatively set LOKI_USE_LEGACY_ROUTES.").
			Default("false").
			Envar("LOKI_USE_LEGACY_ROUTES").
			BoolVar(&r.ClientConfig.UseLegacyRoutes)

		c.Flag("tls-ca-path", "TLS CA certificate to verify Loki API as part of mTLS, alternatively set LOKI_TLS_CA_PATH.").
			Default("").
			Envar("LOKI_TLS_CA_CERT").
			StringVar(&r.ClientConfig.TLS.CAPath)

		c.Flag("tls-cert-path", "TLS client certificate to authenticate with Loki API as part of mTLS, alternatively set Loki_TLS_CERT_PATH.").
			Default("").
			Envar("LOKI_TLS_CLIENT_CERT").
			StringVar(&r.ClientConfig.TLS.CertPath)

		c.Flag("tls-key-path", "TLS client certificate private key to authenticate with Loki API as part of mTLS, alternatively set LOKI_TLS_KEY_PATH.").
			Default("").
			Envar("LOKI_TLS_CLIENT_KEY").
			StringVar(&r.ClientConfig.TLS.KeyPath)

	}

	// Print Rules Command
	printRulesCmd.Flag("disable-color", "disable colored output").BoolVar(&r.DisableColor)

	// Get RuleGroup Command
	getRuleGroupCmd.Arg("namespace", "Namespace of the rulegroup to retrieve.").Required().StringVar(&r.Namespace)
	getRuleGroupCmd.Arg("group", "Name of the rulegroup ot retrieve.").Required().StringVar(&r.RuleGroup)
	getRuleGroupCmd.Flag("disable-color", "disable colored output").BoolVar(&r.DisableColor)

	// Delete RuleGroup Command
	deleteRuleGroupCmd.Arg("namespace", "Namespace of the rulegroup to delete.").Required().StringVar(&r.Namespace)
	deleteRuleGroupCmd.Arg("group", "Name of the rulegroup ot delete.").Required().StringVar(&r.RuleGroup)

	// Load Rules Command
	loadRulesCmd.Arg("rule-files", "The rule files to check.").Required().ExistingFilesVar(&r.RuleFilesList)

	// Diff Command
	diffRulesCmd.Arg("rule-files", "The rule files to check.").ExistingFilesVar(&r.RuleFilesList)
	diffRulesCmd.Flag("namespaces", "comma-separated list of namespaces to check during a diff. Cannot be used together with --ignored-namespaces.").StringVar(&r.Namespaces)
	diffRulesCmd.Flag("ignored-namespaces", "comma-separated list of namespaces to ignore during a diff. Cannot be used together with --namespaces.").StringVar(&r.IgnoredNamespaces)
	diffRulesCmd.Flag("rule-files", "The rule files to check. Flag can be reused to load multiple files.").StringVar(&r.RuleFiles)
	diffRulesCmd.Flag(
		"rule-dirs",
		"Comma separated list of paths to directories containing rules yaml files. Each file in a directory with a .yml or .yaml suffix will be parsed.",
	).StringVar(&r.RuleFilesPath)
	diffRulesCmd.Flag("disable-color", "disable colored output").BoolVar(&r.DisableColor)
	diffRulesCmd.Flag("verbose", "show diff output with rules changes").BoolVar(&r.Verbose)

	// Sync Command
	syncRulesCmd.Arg("rule-files", "The rule files to check.").ExistingFilesVar(&r.RuleFilesList)
	syncRulesCmd.Flag("namespaces", "comma-separated list of namespaces to check during a diff. Cannot be used together with --ignored-namespaces.").StringVar(&r.Namespaces)
	syncRulesCmd.Flag("ignored-namespaces", "comma-separated list of namespaces to ignore during a sync. Cannot be used together with --namespaces.").StringVar(&r.IgnoredNamespaces)
	syncRulesCmd.Flag("rule-files", "The rule files to check. Flag can be reused to load multiple files.").StringVar(&r.RuleFiles)
	syncRulesCmd.Flag(
		"rule-dirs",
		"Comma separated list of paths to directories containing rules yaml files. Each file in a directory with a .yml or .yaml suffix will be parsed.",
	).StringVar(&r.RuleFilesPath)

	// Prepare Command
	prepareCmd.Arg("rule-files", "The rule files to check.").ExistingFilesVar(&r.RuleFilesList)
	prepareCmd.Flag("rule-files", "The rule files to check. Flag can be reused to load multiple files.").StringVar(&r.RuleFiles)
	prepareCmd.Flag(
		"rule-dirs",
		"Comma separated list of paths to directories containing rules yaml files. Each file in a directory with a .yml or .yaml suffix will be parsed.",
	).StringVar(&r.RuleFilesPath)
	prepareCmd.Flag(
		"in-place",
		"edits the rule file in place",
	).Short('i').BoolVar(&r.InPlaceEdit)
	prepareCmd.Flag("label", "label to include as part of the aggregations.").Default(defaultPrepareAggregationLabel).Short('l').StringVar(&r.AggregationLabel)
	prepareCmd.Flag("label-excluded-rule-groups", "Comma separated list of rule group names to exclude when including the configured label to aggregations.").StringVar(&r.AggregationLabelExcludedRuleGroups)

	// Lint Command
	lintCmd.Arg("rule-files", "The rule files to check.").ExistingFilesVar(&r.RuleFilesList)
	lintCmd.Flag("rule-files", "The rule files to check. Flag can be reused to load multiple files.").StringVar(&r.RuleFiles)
	lintCmd.Flag(
		"rule-dirs",
		"Comma separated list of paths to directories containing rules yaml files. Each file in a directory with a .yml or .yaml suffix will be parsed.",
	).StringVar(&r.RuleFilesPath)
	lintCmd.Flag("dry-run", "Performs a trial run that doesn't make any changes and (mostly) produces the same outpupt as a real run.").Short('n').BoolVar(&r.LintDryRun)

	// Check Command
	checkCmd.Arg("rule-files", "The rule files to check.").ExistingFilesVar(&r.RuleFilesList)
	checkCmd.Flag("rule-files", "The rule files to check. Flag can be reused to load multiple files.").StringVar(&r.RuleFiles)
	checkCmd.Flag(
		"rule-dirs",
		"Comma separated list of paths to directories containing rules yaml files. Each file in a directory with a .yml or .yaml suffix will be parsed.",
	).StringVar(&r.RuleFilesPath)
	checkCmd.Flag("strict", "fails rules checks that do not match best practices exactly").BoolVar(&r.Strict)

	// List Command
	listCmd.Flag("format", "Backend type to interact with: <json|yaml|table>").Default("table").EnumVar(&r.Format, formats...)
	listCmd.Flag("disable-color", "disable colored output").BoolVar(&r.DisableColor)
}

func (r *RuleCommand) setup(_ *kingpin.ParseContext) error {
	prometheus.MustRegister(
		ruleLoadTimestamp,
		ruleLoadSuccessTimestamp,
	)

	// Loki's non-legacy route does not match Cortex, but the legacy one does.
	if r.Backend == rules.LokiBackend {
		r.ClientConfig.UseLegacyRoutes = true
	}

	cli, err := client.New(r.ClientConfig)
	if err != nil {
		return err
	}
	r.cli = cli

	return nil
}

func (r *RuleCommand) setupFiles() error {
	if r.Namespaces != "" && r.IgnoredNamespaces != "" {
		return errors.New("--namespaces and --ignored-namespaces cannot be set at the same time")
	}

	// Set up ignored namespaces map for sync/diff command
	if r.IgnoredNamespaces != "" {
		r.ignoredNamespacesMap = map[string]struct{}{}
		for _, ns := range strings.Split(r.IgnoredNamespaces, ",") {
			if ns != "" {
				r.ignoredNamespacesMap[ns] = struct{}{}
			}
		}
	}

	// Set up allowed namespaces map for sync/diff command
	if r.Namespaces != "" {
		r.namespacesMap = map[string]struct{}{}
		for _, ns := range strings.Split(r.Namespaces, ",") {
			if ns != "" {
				r.namespacesMap[ns] = struct{}{}
			}
		}
	}

	// Set up rule groups excluded from label aggregation.
	r.aggregationLabelExcludedRuleGroupsList = map[string]struct{}{}
	for _, name := range strings.Split(r.AggregationLabelExcludedRuleGroups, ",") {
		if name = strings.TrimSpace(name); name != "" {
			r.aggregationLabelExcludedRuleGroupsList[name] = struct{}{}
		}
	}

	for _, file := range strings.Split(r.RuleFiles, ",") {
		if file != "" {
			log.WithFields(log.Fields{
				"file": file,
			}).Debugf("adding file")
			r.RuleFilesList = append(r.RuleFilesList, file)
		}
	}

	for _, dir := range strings.Split(r.RuleFilesPath, ",") {
		if dir != "" {
			err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}

				if strings.HasSuffix(info.Name(), ".yml") || strings.HasSuffix(info.Name(), ".yaml") {
					log.WithFields(log.Fields{
						"file": info.Name(),
						"path": path,
					}).Debugf("adding file in rule-path")
					r.RuleFilesList = append(r.RuleFilesList, path)
					return nil
				}
				log.WithFields(log.Fields{
					"file": info.Name(),
					"path": path,
				}).Debugf("ignorings file in rule-path")
				return nil
			})
			if err != nil {
				return fmt.Errorf("error walking the path %q: %v", dir, err)
			}
		}
	}

	return nil
}

func (r *RuleCommand) listRules(_ *kingpin.ParseContext) error {
	rules, err := r.cli.ListRules(context.Background(), "")
	if err != nil {
		log.Fatalf("unable to read rules from loki, %v", err)

	}

	p := printer.New(r.DisableColor)
	return p.PrintRuleSet(rules, r.Format, os.Stdout)
}

func (r *RuleCommand) printRules(_ *kingpin.ParseContext) error {
	rules, err := r.cli.ListRules(context.Background(), "")
	if err != nil {
		if err == client.ErrResourceNotFound {
			log.Infof("no rule groups currently exist for this user")
			return nil
		}
		log.Fatalf("unable to read rules from loki, %v", err)
	}

	p := printer.New(r.DisableColor)
	return p.PrintRuleGroups(rules)
}

func (r *RuleCommand) getRuleGroup(_ *kingpin.ParseContext) error {
	group, err := r.cli.GetRuleGroup(context.Background(), r.Namespace, r.RuleGroup)
	if err != nil {
		if err == client.ErrResourceNotFound {
			log.Infof("this rule group does not currently exist")
			return nil
		}
		log.Fatalf("unable to read rules from loki, %v", err)
	}

	p := printer.New(r.DisableColor)
	return p.PrintRuleGroup(*group)
}

func (r *RuleCommand) deleteRuleGroup(_ *kingpin.ParseContext) error {
	err := r.cli.DeleteRuleGroup(context.Background(), r.Namespace, r.RuleGroup)
	if err != nil && err != client.ErrResourceNotFound {
		log.Fatalf("unable to delete rule group from loki, %v", err)
	}
	return nil
}

func (r *RuleCommand) loadRules(_ *kingpin.ParseContext) error {
	nss, err := rules.ParseFiles(r.RuleFilesList)
	if err != nil {
		return errors.Wrap(err, "load operation unsuccessful, unable to parse rules files")
	}
	ruleLoadTimestamp.SetToCurrentTime()

	for _, ns := range nss {
		for _, group := range ns.Groups {
			fmt.Printf("group: '%v', ns: '%v'\n", group.Name, ns.Namespace)
			curGroup, err := r.cli.GetRuleGroup(context.Background(), ns.Namespace, group.Name)
			if err != nil && err != client.ErrResourceNotFound {
				return errors.Wrap(err, "load operation unsuccessful, unable to contact the loki api")
			}
			if curGroup != nil {
				err = rules.CompareGroups(*curGroup, group)
				if err == nil {
					log.WithFields(log.Fields{
						"group":     group.Name,
						"namespace": ns.Namespace,
					}).Infof("group already exists")
					continue
				}
				log.WithFields(log.Fields{
					"group":      group.Name,
					"namespace":  ns.Namespace,
					"difference": err,
				}).Infof("updating group")
			}

			err = r.cli.CreateRuleGroup(context.Background(), ns.Namespace, group)
			if err != nil {
				log.WithError(err).WithFields(log.Fields{
					"group":     group.Name,
					"namespace": ns.Namespace,
				}).Errorf("unable to load rule group")
				return fmt.Errorf("load operation unsuccessful")
			}
		}
	}

	ruleLoadSuccessTimestamp.SetToCurrentTime()
	return nil
}

// shouldCheckNamespace returns whether the namespace should be checked according to the allowed and ignored namespaces
func (r *RuleCommand) shouldCheckNamespace(namespace string) bool {
	// when we have an allow list, only check those that we have explicitly defined.
	if r.namespacesMap != nil {
		_, allowed := r.namespacesMap[namespace]
		return allowed
	}

	_, ignored := r.ignoredNamespacesMap[namespace]
	return !ignored
}

func (r *RuleCommand) diffRules(_ *kingpin.ParseContext) error {
	err := r.setupFiles()
	if err != nil {
		return errors.Wrap(err, "diff operation unsuccessful, unable to load rules files")
	}

	nss, err := rules.ParseFiles(r.RuleFilesList)
	if err != nil {
		return errors.Wrap(err, "diff operation unsuccessful, unable to parse rules files")
	}

	currentNamespaceMap, err := r.cli.ListRules(context.Background(), "")
	//TODO: Skipping the 404s here might end up in an unsual scenario.
	// If we're unable to reach the Loki API due to a bad URL, we'll assume no rules are
	// part of the namespace and provide a diff of the whole ruleset.
	if err != nil && err != client.ErrResourceNotFound {
		return errors.Wrap(err, "diff operation unsuccessful, unable to contact the loki api")
	}

	changes := []rules.NamespaceChange{}

	for _, ns := range nss {
		if !r.shouldCheckNamespace(ns.Namespace) {
			continue
		}

		currentNamespace, exists := currentNamespaceMap[ns.Namespace]
		if !exists {
			changes = append(changes, rules.NamespaceChange{
				State:         rules.Created,
				Namespace:     ns.Namespace,
				GroupsCreated: ns.Groups,
			})
			continue
		}

		origNamespace := rules.RuleNamespace{
			Namespace: ns.Namespace,
			Groups:    currentNamespace,
		}

		changes = append(changes, rules.CompareNamespaces(origNamespace, ns))

		// Remove namespace from temp map so namespaces that have been removed can easily be detected
		delete(currentNamespaceMap, ns.Namespace)
	}

	for ns, deletedGroups := range currentNamespaceMap {
		if !r.shouldCheckNamespace(ns) {
			continue
		}

		changes = append(changes, rules.NamespaceChange{
			State:         rules.Deleted,
			Namespace:     ns,
			GroupsDeleted: deletedGroups,
		})
	}

	p := printer.New(r.DisableColor)
	return p.PrintComparisonResult(changes, r.Verbose)
}

func (r *RuleCommand) syncRules(_ *kingpin.ParseContext) error {
	err := r.setupFiles()
	if err != nil {
		return errors.Wrap(err, "sync operation unsuccessful, unable to load rules files")
	}

	nss, err := rules.ParseFiles(r.RuleFilesList)
	if err != nil {
		return errors.Wrap(err, "sync operation unsuccessful, unable to parse rules files")
	}

	currentNamespaceMap, err := r.cli.ListRules(context.Background(), "")
	//TODO: Skipping the 404s here might end up in an unsual scenario.
	// If we're unable to reach the Loki API due to a bad URL, we'll assume no rules are
	// part of the namespace and provide a diff of the whole ruleset.
	if err != nil && err != client.ErrResourceNotFound {
		return errors.Wrap(err, "sync operation unsuccessful, unable to contact the loki api")
	}

	changes := []rules.NamespaceChange{}

	for _, ns := range nss {
		if !r.shouldCheckNamespace(ns.Namespace) {
			continue
		}

		currentNamespace, exists := currentNamespaceMap[ns.Namespace]
		if !exists {
			changes = append(changes, rules.NamespaceChange{
				State:         rules.Created,
				Namespace:     ns.Namespace,
				GroupsCreated: ns.Groups,
			})
			continue
		}

		origNamespace := rules.RuleNamespace{
			Namespace: ns.Namespace,
			Groups:    currentNamespace,
		}

		changes = append(changes, rules.CompareNamespaces(origNamespace, ns))

		// Remove namespace from temp map so namespaces that have been removed can easily be detected
		delete(currentNamespaceMap, ns.Namespace)
	}

	for ns, deletedGroups := range currentNamespaceMap {
		if !r.shouldCheckNamespace(ns) {
			continue
		}

		changes = append(changes, rules.NamespaceChange{
			State:         rules.Deleted,
			Namespace:     ns,
			GroupsDeleted: deletedGroups,
		})
	}

	err = r.executeChanges(context.Background(), changes)
	if err != nil {
		return errors.Wrap(err, "sync operation unsuccessful, unable to complete executing changes")
	}

	return nil
}

func (r *RuleCommand) executeChanges(ctx context.Context, changes []rules.NamespaceChange) error {
	var err error
	for _, ch := range changes {
		for _, g := range ch.GroupsCreated {
			if !r.shouldCheckNamespace(ch.Namespace) {
				continue
			}

			log.WithFields(log.Fields{
				"group":     g.Name,
				"namespace": ch.Namespace,
			}).Infof("creating group")
			err = r.cli.CreateRuleGroup(ctx, ch.Namespace, g)
			if err != nil {
				return err
			}
		}

		for _, g := range ch.GroupsUpdated {
			if !r.shouldCheckNamespace(ch.Namespace) {
				continue
			}

			log.WithFields(log.Fields{
				"group":     g.New.Name,
				"namespace": ch.Namespace,
			}).Infof("updating group")
			err = r.cli.CreateRuleGroup(ctx, ch.Namespace, g.New)
			if err != nil {
				return err
			}
		}

		for _, g := range ch.GroupsDeleted {
			if !r.shouldCheckNamespace(ch.Namespace) {
				continue
			}

			log.WithFields(log.Fields{
				"group":     g.Name,
				"namespace": ch.Namespace,
			}).Infof("deleting group")
			err = r.cli.DeleteRuleGroup(ctx, ch.Namespace, g.Name)
			if err != nil && err != client.ErrResourceNotFound {
				return err
			}
		}
	}

	updated, created, deleted := rules.SummarizeChanges(changes)
	fmt.Println()
	fmt.Printf("Sync Summary: %v Groups Created, %v Groups Updated, %v Groups Deleted\n", created, updated, deleted)
	return nil
}

func (r *RuleCommand) prepare(_ *kingpin.ParseContext) error {
	err := r.setupFiles()
	if err != nil {
		return errors.Wrap(err, "prepare operation unsuccessful, unable to load rules files")
	}

	namespaces, err := rules.ParseFiles(r.RuleFilesList)
	if err != nil {
		return errors.Wrap(err, "prepare operation unsuccessful, unable to parse rules files")
	}

	// Do not apply the aggregation label to excluded rule groups.
	applyTo := func(group rwrulefmt.RuleGroup, _ rulefmt.RuleNode) bool {
		_, excluded := r.aggregationLabelExcludedRuleGroupsList[group.Name]
		return !excluded
	}

	var count, mod int
	for _, ruleNamespace := range namespaces {
		c, m, err := ruleNamespace.AggregateBy(r.AggregationLabel, applyTo)
		if err != nil {
			return err
		}

		count += c
		mod += m
	}

	// now, save all the files
	if err := save(namespaces, r.InPlaceEdit); err != nil {
		return err
	}

	log.Infof("SUCCESS: %d rules found, %d modified expressions", count, mod)

	return nil
}

func (r *RuleCommand) lint(_ *kingpin.ParseContext) error {
	err := r.setupFiles()
	if err != nil {
		return errors.Wrap(err, "prepare operation unsuccessful, unable to load rules files")
	}

	namespaces, err := rules.ParseFiles(r.RuleFilesList)
	if err != nil {
		return errors.Wrap(err, "prepare operation unsuccessful, unable to parse rules files")
	}

	var count, mod int
	for _, ruleNamespace := range namespaces {
		c, m, err := ruleNamespace.LintExpressions()
		if err != nil {
			return err
		}

		count += c
		mod += m
	}

	if !r.LintDryRun {
		// linting will always in-place edit unless is a dry-run.
		if err := save(namespaces, true); err != nil {
			return err
		}
	}

	log.Infof("SUCCESS: %d rules found, %d linted expressions", count, mod)

	return nil
}

func (r *RuleCommand) checkRecordingRuleNames(_ *kingpin.ParseContext) error {
	err := r.setupFiles()
	if err != nil {
		return errors.Wrap(err, "check operation unsuccessful, unable to load rules files")
	}

	namespaces, err := rules.ParseFiles(r.RuleFilesList)
	if err != nil {
		return errors.Wrap(err, "check operation unsuccessful, unable to parse rules files")
	}

	for _, ruleNamespace := range namespaces {
		n := ruleNamespace.CheckRecordingRules(r.Strict)
		if n != 0 {
			return fmt.Errorf("%d erroneous recording rule names", n)
		}
		duplicateRules := checkDuplicates(ruleNamespace.Groups)
		if len(duplicateRules) != 0 {
			fmt.Printf("%d duplicate rule(s) found.\n", len(duplicateRules))
			for _, n := range duplicateRules {
				fmt.Printf("Metric: %s\nLabel(s):\n", n.metric)
				for i, l := range n.label {
					fmt.Printf("\t%s: %s\n", i, l)
				}
			}
			fmt.Println("Might cause inconsistency while recording expressions.")
		}
	}

	return nil
}

// Taken from https://github.com/prometheus/prometheus/blob/8c8de46003d1800c9d40121b4a5e5de8582ef6e1/cmd/promtool/main.go#L403
type compareRuleType struct {
	metric string
	label  map[string]string
}

func checkDuplicates(groups []rwrulefmt.RuleGroup) []compareRuleType {
	var duplicates []compareRuleType

	for _, group := range groups {
		for index, rule := range group.Rules {
			inst := compareRuleType{
				metric: ruleMetric(rule),
				label:  rule.Labels,
			}
			for i := 0; i < index; i++ {
				t := compareRuleType{
					metric: ruleMetric(group.Rules[i]),
					label:  group.Rules[i].Labels,
				}
				if reflect.DeepEqual(t, inst) {
					duplicates = append(duplicates, t)
				}
			}
		}
	}
	return duplicates
}

func ruleMetric(rule rulefmt.RuleNode) string {
	if rule.Alert.Value != "" {
		return rule.Alert.Value
	}
	return rule.Record.Value
}

// End taken from https://github.com/prometheus/prometheus/blob/8c8de46003d1800c9d40121b4a5e5de8582ef6e1/cmd/promtool/main.go#L403

// save saves a set of rule files to to disk. You can specify whenever you want the
// file(s) to be edited in-place.
func save(nss map[string]rules.RuleNamespace, i bool) error {
	for _, ns := range nss {
		payload, err := yamlv3.Marshal(ns)
		if err != nil {
			return err
		}

		filepath := ns.Filepath
		if !i {
			filepath = filepath + ".result"
		}

		if err := os.WriteFile(filepath, payload, 0644); err != nil {
			return err
		}
	}

	return nil
}
