package magefile

import (
	"os"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"github.com/pkg/errors"
)

type Assets mg.Namespace

func (Assets) CheckAssets() error {
	err := sh.Run("git", "diff", "--exit-code", "pkg/promtail/server/ui")
	if err != nil {
		return errors.Wrap(err, "assets have been changed, run `mage assets:generateAssets` and then commit changes to continue")
	}
	return err
}

func (Assets) GenerateAssets() error {
	return sh.RunWith(
		map[string]string{
			"GOOS": os.Getenv("GOOS"),
		},
		"go", "generate", "-x", "-v", "./pkg/promtail/server/ui",
	)
}
