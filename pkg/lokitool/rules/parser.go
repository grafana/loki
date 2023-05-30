package rules

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v3"

	"github.com/grafana/loki/pkg/ruler"
)

var (
	errFileReadError  = errors.New("file read error")
	errInvalidBackend = errors.New("invalid backend type")
)

// ParseFiles returns a formatted set of prometheus rule groups
func ParseFiles(files []string) (map[string]RuleNamespace, error) {
	ruleSet := map[string]RuleNamespace{}
	for _, f := range files {
		nss, errs := ParseLoki(f)
		for _, err := range errs {
			log.WithError(err).WithField("file", f).Errorln("unable parse rules file")
			return nil, errFileReadError
		}

		for _, ns := range nss {
			ns.Filepath = f

			// Determine if the namespace is explicitly set. If not
			// the file name without the extension is used.
			namespace := ns.Namespace
			if namespace == "" {
				namespace = strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
				ns.Namespace = namespace
			}

			_, exists := ruleSet[namespace]
			if exists {
				log.WithFields(log.Fields{
					"namespace": namespace,
					"file":      f,
				}).Errorln("repeated namespace attempted to be loaded")
				return nil, errFileReadError
			}
			ruleSet[namespace] = ns
		}
	}
	return ruleSet, nil
}

// Parse parses and validates a set of rules.
func Parse(f string) ([]RuleNamespace, []error) {
	content, err := loadFile(f)
	if err != nil {
		log.WithError(err).WithField("file", f).Errorln("unable load rules file")
		return nil, []error{errFileReadError}
	}

	return ParseBytes(content)
}

func ParseBytes(content []byte) ([]RuleNamespace, []error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	var nss []RuleNamespace
	for {
		var ns RuleNamespace
		err := decoder.Decode(&ns)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, []error{err}
		}

		if errs := ns.Validate(); len(errs) > 0 {
			return nil, errs
		}

		nss = append(nss, ns)
	}
	return nss, nil
}

func ParseLoki(f string) ([]RuleNamespace, []error) {
	content, err := loadFile(f)
	if err != nil {
		log.WithError(err).WithField("file", f).Errorln("unable load rules file")
		return nil, []error{errFileReadError}
	}

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	var nss []RuleNamespace
	for {
		var ns RuleNamespace
		err := decoder.Decode(&ns)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, []error{err}
		}

		if errs := ruler.ValidateGroups(ns.Groups...); len(errs) > 0 {
			return nil, errs
		}

		nss = append(nss, ns)

	}
	return nss, nil
}

func loadFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileinfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	filesize := fileinfo.Size()
	buffer := make([]byte, filesize)

	_, err = file.Read(buffer)
	if err != nil {
		return nil, err
	}

	return buffer, nil
}
