// This little tool is used to generate prometheus test files from the templates
package main

import (
	"embed"
	"fmt"
	"os"
	"path"
	"text/template"

	"github.com/ViaQ/logerr/log"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/internal/alerts"
)

const (
	// exitFail is the exit code if the program fails.
	exitFail = 1
)

var (
	baseOutputDir = "internal/manifests/internal/alerts/test/generated"

	//go:embed test.yaml.tmpl
	testTmplFile embed.FS

	testTmpl = template.Must(template.New("test.yaml.tmpl").Delims("[[", "]]").ParseFS(testTmplFile, "test.yaml.tmpl"))
)

func init() {
	log.Init("test-templates")
}

func main() {
	if err := run(); err != nil {
		log.Error(err, "Error running test-templates")
		os.Exit(exitFail)
	}
	log.Info("Done")
}

func run() error {
	log.Info("starting test-templates")

	sizes := []lokiv1beta1.LokiStackSizeType{
		lokiv1beta1.SizeOneXExtraSmall,
		lokiv1beta1.SizeOneXSmall,
		lokiv1beta1.SizeOneXMedium,
	}

	for _, s := range sizes {
		opts := manifests.Options{Stack: lokiv1beta1.LokiStackSpec{Size: s}}
		aopts := manifests.AlertsOptions(opts)

		alertsBytes, rulesBytes, err := alerts.Build(aopts)
		if err != nil {
			return err
		}
		alertsFileName := fmt.Sprintf("alerts-%s.yaml", s)
		rulesFileName := fmt.Sprintf("rules-%s.yaml", s)
		alertsFilePath := path.Join(baseOutputDir, alertsFileName)
		rulesFilePath := path.Join(baseOutputDir, rulesFileName)
		log.WithValues("path", alertsFilePath).Info("Writing file")
		err = os.WriteFile(alertsFilePath, alertsBytes, 0o644)
		if err != nil {
			return err
		}
		log.WithValues("path", rulesFilePath).Info("Writing file")
		err = os.WriteFile(rulesFilePath, rulesBytes, 0o644)
		if err != nil {
			return err
		}

		err = createTestFile(aopts)
		if err != nil {
			return err
		}
	}

	return nil
}

func createTestFile(aopts alerts.Options) (err error) {
	testFileName := fmt.Sprintf("test-%s.yaml", aopts.Stack.Size)
	testFilePath := path.Join(baseOutputDir, testFileName)
	var f *os.File
	f, err = os.Create(testFilePath)
	if err != nil {
		return err
	}

	// Don't defer Close() on writable files
	// https://www.joeshaw.org/dont-defer-close-on-writable-files/
	defer func() {
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
	}()
	log.WithValues("path", testFilePath).Info("Writing file")
	err = testTmpl.Execute(f, aopts)
	if err != nil {
		return err
	}
	return nil
}
