package magefile

import "github.com/magefile/mage/sh"

var (
	helm    = sh.RunCmd("helm")
	git     = sh.RunCmd("git")
	kubectl = sh.RunCmd("kubectl")
)
