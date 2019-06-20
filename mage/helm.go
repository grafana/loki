package magefile

import (
	"fmt"
	"log"
	"os"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

type Helm mg.Namespace

var CHARTS = [...]string{"production/helm/loki", "production/helm/promtail", "production/helm/loki-stack"}

func (Helm) Build() error {
	if err := rmGlob("production/helm/*/requirements.lock"); err != nil {
		return err
	}
	if err := helm("init", "-c"); err != nil {
		return err
	}

	for _, chart := range CHARTS {
		if err := helm("dependency", "build", chart); err != nil {
			return err
		}
		if err := helm("lint", chart); err != nil {
			return err
		}
		if err := helm("package", chart); err != nil {
			return err
		}
	}

	if err := rmGlob("production/helm/*/requirements.lock"); err != nil {
		return err
	}

	return nil
}

func (Helm) Publish() error {
	if err := sh.Copy("production/helm/README.md", "index.md"); err != nil {
		return err
	}

	if os.Getenv("CI") != "" {
		log.Println("Running in CI, setting up git")
		user := os.Getenv("CIRCLE_USERNAME")
		if err := git("config", "user.email", fmt.Sprintf("%v@users.noreply.github.com", user)); err != nil {
			return err
		}
		if err := git("config", "user.name", user); err != nil {
			return err
		}
	}

	if err := git("checkout", "gh-pages"); err != nil {
		log.Println("gh-pages checkout failed, switching to orphan mode")
		if err := git("checkout", "--orphan", "gh-pages"); err != nil {
			return err
		}
		if err := sh.Rm("."); err != nil {
			return err
		}
	}
	if _, err := os.Stat("charts"); os.IsNotExist(err) {
		if err := os.Mkdir("charts", 0755); err != nil {
			return err
		}
	}

	if err := mvGlobToDir("*.tgz", "charts"); err != nil {
		return err
	}
	if err := os.Rename("index.md", "charts/index.md"); err != nil {
		return err
	}

	if err := helm("repo", "index", "charts/"); err != nil {
		return err
	}

	if err := git("add", "charts"); err != nil {
		return err
	}

	if err := git("commit", "-m",
		fmt.Sprintf("[skip ci] Publishing helm charts: %v", os.Getenv("CIRCLE_SHA1")),
	); err != nil {
		return err
	}

	if err := git("push", "origin", "gh-pages"); err != nil {
		return err
	}
	return nil
}

func (h Helm) Install() error {
	if err := kubectl("apply", "-f", "tools/helm.yaml"); err != nil {
		return err
	}

	if err := helm("init", "--wait", "--service-account=helm", "--upgrade"); err != nil {
		return err
	}

	return h.upgrade()
}

func (h Helm) Debug() error {
	return h.upgrade("--dry-run", "--debug")
}

func (h Helm) Upgrade() error {
	return h.upgrade()
}

func (Helm) upgrade(flags ...string) error {
	tag := func(name string) string {
		return name
	}

	args := []string{"upgrade", "--wait", "--install", "-f=tools/dev.values.yaml"}
	args = append(args, flags...)
	args = append(args, "loki-stack", "./production/helm/loki-stack")
	args = append(args, []string{
		"--set", tag("promtail"),
		"--set", tag("loki"),
	}...)

	if err := helm(args...); err != nil {
		return err
	}
	return nil
}

func (Helm) Clean() error {
	return helm("delete", "--purge", "loki-stack")
}
