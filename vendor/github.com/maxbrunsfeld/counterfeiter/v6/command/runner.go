package command

import (
	"fmt"
	"go/build"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func Detect(cwd string, args []string, generateMode bool) ([]Invocation, error) {
	if generateMode || invokedByGoGenerate() {
		return invocations(cwd, generateMode)
	}
	i, err := NewInvocation("", 0, args)
	if err != nil {
		return nil, err
	}
	return []Invocation{i}, nil
}

type Invocation struct {
	Args []string
	Line int
	File string
}

func NewInvocation(file string, line int, args []string) (Invocation, error) {
	if len(args) < 1 {
		return Invocation{}, fmt.Errorf("%s:%v an invocation of counterfeiter must have arguments", file, line)
	}
	i := Invocation{
		File: file,
		Line: line,
		Args: args,
	}
	return i, nil
}

func invokedByGoGenerate() bool {
	return os.Getenv("DOLLAR") == "$"
}

func invocations(cwd string, generateMode bool) ([]Invocation, error) {
	var result []Invocation
	// Find all the go files
	pkg, err := build.ImportDir(cwd, build.IgnoreVendor)
	if err != nil {
		return nil, err
	}

	gofiles := make([]string, 0, len(pkg.GoFiles)+len(pkg.CgoFiles)+len(pkg.TestGoFiles)+len(pkg.XTestGoFiles))
	gofiles = append(gofiles, pkg.GoFiles...)
	gofiles = append(gofiles, pkg.CgoFiles...)
	gofiles = append(gofiles, pkg.TestGoFiles...)
	gofiles = append(gofiles, pkg.XTestGoFiles...)
	sort.Strings(gofiles)
	var line int
	if !generateMode {
		// generateMode means counterfeiter:generate, not go:generate
		line, err = strconv.Atoi(os.Getenv("GOLINE"))
		if err != nil {
			return nil, err
		}
	}

	for i := range gofiles {
		i, err := open(cwd, gofiles[i], generateMode)
		if err != nil {
			return nil, err
		}
		result = append(result, i...)
		if generateMode {
			continue
		}
		if len(result) > 0 && result[0].File != os.Getenv("GOFILE") {
			return nil, nil
		}
		if len(result) > 0 && result[0].Line != line {
			return nil, nil
		}
	}

	return result, nil
}

var directive = regexp.MustCompile(`(?mi)^//(go:generate|counterfeiter:generate)\s*(.*)?\s*$`)
var args = regexp.MustCompile(`(?mi)^(?:go run github\.com/maxbrunsfeld/counterfeiter/v6|gobin -m -run github\.com/maxbrunsfeld/counterfeiter/v6|counterfeiter|counterfeiter.exe)\s*(.*)?\s*$`)

type match struct {
	directive string
	args      []string
}

func matchForString(s string) *match {
	m := directive.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	if m[1] == "counterfeiter:generate" {
		return &match{
			directive: m[1],
			args:      stringToArgs(m[2]),
		}
	}
	m2 := args.FindStringSubmatch(m[2])
	if m2 == nil {
		return nil
	}
	return &match{
		directive: m[1],
		args:      stringToArgs(m2[1]),
	}
}

func open(dir string, file string, generateMode bool) ([]Invocation, error) {
	str, err := ioutil.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(str), "\n")

	var result []Invocation
	line := 0
	for i := range lines {
		line++
		match := matchForString(lines[i])
		if match == nil {
			continue
		}
		inv, err := NewInvocation(file, line, match.args)
		if err != nil {
			return nil, err
		}

		if generateMode && match.directive == "counterfeiter:generate" {
			result = append(result, inv)
		}

		if !generateMode && match.directive == "go:generate" {
			if len(inv.Args) == 2 && strings.EqualFold(strings.TrimSpace(inv.Args[1]), "-generate") {
				continue
			}
			result = append(result, inv)
		}
	}

	return result, nil
}

func stringToArgs(s string) []string {
	a := strings.Fields(s)
	result := []string{
		"counterfeiter",
	}
	result = append(result, a...)
	return result
}
