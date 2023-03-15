package analyze

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ctxKey int

const analyzeKey ctxKey = 0

type Context struct {
	countIn  int           `json:"countIn,omitempty"`
	countOut int           `json:"countOut,omitempty"`
	duration time.Duration `json:"duration,omitempty"`

	name        string `json:"name,omitempty"`
	description string `json:"description,omitempty"`
	index       int    `json:"index,omitempty"`

	childContexts []*Context `json:"children,omitempty"`
}

func (ctx *Context) AddChild(child *Context) {
	ctx.childContexts = append(ctx.childContexts, child)
}

// walks the contexts children recursively and adds them
// so that hopefully things are nested properly and we can
// print them nicely
func (ctx *Context) AddChildRecursively(child *Context) {
	newCtx := Context{
		countIn:     child.countIn,
		countOut:    child.countOut,
		name:        child.name,
		description: child.description,
		duration:    child.duration,
	}
	for _, c := range child.childContexts {
		newCtx.AddChildRecursively(c)
	}
	ctx.AddChild(&newCtx)
}

func (ctx *Context) GetChild(index int) *Context {
	if len(ctx.childContexts) > index {
		return ctx.childContexts[index]
	}
	return nil
}

func (ctx *Context) GetCounts() (int, int) {
	return ctx.countIn, ctx.countOut
}

func (ctx *Context) Observe(d time.Duration, match bool) {
	ctx.countIn++
	if match {
		ctx.countOut++
	}
	ctx.duration += d
}

func (ctx *Context) String() string {
	e := new(strings.Builder)
	fmt.Fprintf(e, ctx.baseString())
	for _, child := range ctx.childContexts {
		fmt.Fprintf(e, child.stringNested(1))
	}
	// janky, trim the final newline character for nice println usage
	return e.String()[:e.Len()]
}

func (ctx *Context) baseString() string {
	return fmt.Sprintf("AnalyzeContext{name=%s, desc=%s, in=%d, out=%d, duration=%s, index=%d}\n", ctx.name, ctx.description, ctx.countIn, ctx.countOut, ctx.duration, ctx.index)

}

func (ctx *Context) stringNested(level int) string {
	e := new(strings.Builder)
	fmt.Fprintf(e, fmt.Sprintf("%s%s", strings.Repeat("\t", level), ctx.baseString()))
	for _, child := range ctx.childContexts {
		fmt.Fprintf(e, child.stringNested(level+1))
	}
	return e.String()
}

func (ctx *Context) Reset() {
	ctx.countIn = 0
	ctx.duration = 0
	for idx := range ctx.childContexts {
		ctx.childContexts[idx].Reset()
	}
}

func (ctx *Context) Set(d time.Duration, in, out int) {
	ctx.duration = d
	ctx.countIn = in
	ctx.countOut = out
}

func (ctx *Context) SetDescription(d string) {
	ctx.description = d
}

func (ctx *Context) ToProto() *RemoteContext {
	children := make([]*RemoteContext, len(ctx.childContexts))
	for i, c := range ctx.childContexts {
		children[i] = c.ToProto()
	}
	return &RemoteContext{
		CountIn:     int64(ctx.countIn),
		CountOut:    int64(ctx.countOut),
		Duration:    ctx.duration.String(),
		Index:       int32(ctx.index),
		Description: ctx.description,
		Name:        ctx.name,
		Children:    children,
	}
}

func New(name, description string, index int, size int) *Context {
	return &Context{
		name:          name,
		index:         index,
		description:   description,
		childContexts: make([]*Context, 0, size),
	}
}

func FromProto(c *RemoteContext) *Context {
	children := make([]*Context, len(c.Children))
	for i, child := range c.Children {
		children[i] = FromProto(child)
	}
	d, err := time.ParseDuration(c.Duration)
	if err != nil {
		fmt.Println("error parsing duration from string: ", c.Duration)
	}
	return &Context{
		countOut:      int(c.CountOut),
		countIn:       int(c.CountIn),
		name:          c.Name,
		description:   c.Description,
		index:         int(c.Index),
		duration:      d,
		childContexts: children,
	}
}

func NewContext(ctx context.Context, name, description *string) (*Context, context.Context) {
	n := "root"
	d := ""
	if name != nil {
		n = *name
	}
	if description != nil {
		d = *description
	}
	c := New(n, d, 0, 2)
	return c, ToContext(c, ctx)
}

func ToContext(c *Context, ctx context.Context) context.Context {
	return context.WithValue(ctx, analyzeKey, c)
}

func FromContext(ctx context.Context) *Context {
	v, ok := ctx.Value(analyzeKey).(*Context)
	if !ok {
		return New("root", "", 0, 2)
	}
	return v
}
