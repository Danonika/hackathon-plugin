package mcp

import "context"

type Handler func(ctx context.Context, args map[string]any) (any, error)

type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     Handler
}

type Registry struct {
	tools map[string]*Tool
	order []string
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]*Tool{}}
}

func (r *Registry) Register(t Tool) {
	if _, exists := r.tools[t.Name]; !exists {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = &t
}

func (r *Registry) Get(name string) (*Tool, bool) {
	t, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	copied := *t
	return &copied, true
}

func (r *Registry) List() []*Tool {
	out := make([]*Tool, 0, len(r.order))
	for _, n := range r.order {
		if t, ok := r.tools[n]; ok {
			copied := *t
			out = append(out, &copied)
		}
	}
	return out
}

func (r *Registry) Names() []string {
	return append([]string(nil), r.order...)
}
