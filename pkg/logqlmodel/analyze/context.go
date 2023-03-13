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
	countIn  int
	countOut int
	duration time.Duration

	name  string
	index int

	childContexts []*Context
}

func (ctx *Context) AddChild(child *Context) {
	ctx.childContexts = append(ctx.childContexts, child)
}

func (ctx *Context) GetChild(index int) *Context {
	return ctx.childContexts[index]
}

func (ctx *Context) Observe(d time.Duration, match bool) {
	ctx.countIn++
	if match {
		ctx.countOut++
	}
	ctx.duration += d
}

func (ctx *Context) String() string {
	children := make([]string, 0, len(ctx.childContexts))
	for _, child := range ctx.childContexts {
		children = append(children, child.String())
	}
	return fmt.Sprintf("AnalyzeContext{name=%s, in=%d, out=%d, duration=%s, index=%d, children=[%s]}", ctx.name, ctx.countIn, ctx.countOut, ctx.duration, ctx.index, strings.Join(children, ","))
}

func (ctx *Context) Reset() {
	ctx.countIn = 0
	ctx.duration = 0
	for idx := range ctx.childContexts {
		ctx.childContexts[idx].Reset()
	}
}

func New(name string, index int, size int) *Context {
	return &Context{
		name:          name,
		index:         index,
		childContexts: make([]*Context, 0, size),
	}
}

func NewContext(ctx context.Context) (*Context, context.Context) {
	c := New("root", 0, 2)
	return c, ToContext(c, ctx)
}

func ToContext(c *Context, ctx context.Context) context.Context {
	return context.WithValue(ctx, analyzeKey, c)
}

func FromContext(ctx context.Context) *Context {
	v, ok := ctx.Value(analyzeKey).(*Context)
	if !ok {
		return New("root", 0, 2)
	}
	return v
}
