package flagext

import "flag"

type Registerable interface {
	RegisterFlags(*flag.FlagSet)
}

func RegisterConfigs(flagset *flag.FlagSet, cfgs ...Registerable) {
	for _, cfg := range cfgs {
		cfg.RegisterFlags(flagset)
	}
}

func Var(flagset *flag.FlagSet, v flag.Value, name, def, help string) {
	v.Set(def)
	flagset.Var(v, name, help)
}
