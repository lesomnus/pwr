package lab

import (
	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"context"
	"github.com/lesomnus/payday/pdpb"
)

// Frame belongs to one evaluator/goroutine and is not shared across subscribers.
type Frame struct {
	p     cel.Program
	value *view
	vars  map[string]any
}

func NewFrame(p cel.Program, event *pdpb.WatchEvent) *Frame {
	v := &view{}
	return &Frame{p, v, map[string]any{"event": &binding{event, v}}}
}
func (f *Frame) Eval(s Session) (bool, error) {
	f.value.session = s
	v, _, err := f.p.ContextEval(context.Background(), f.vars)
	return v == types.True, err
}
