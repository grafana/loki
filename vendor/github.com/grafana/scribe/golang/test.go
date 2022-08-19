package golang

import (
	"github.com/grafana/scribe"
	"github.com/grafana/scribe/exec"
	"github.com/grafana/scribe/plumbing"
	"github.com/grafana/scribe/plumbing/pipeline"
)

func Test(sw *scribe.Scribe, pkg string) pipeline.Step {
	return pipeline.NewStep(exec.RunAction("go", "test", pkg)).
		WithImage(plumbing.SubImage("go", sw.Opts.Version)).
		WithArguments(pipeline.ArgumentSourceFS)
}
