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
	r.tools[t.Name] = cloneTool(&t)
}

func (r *Registry) Get(name string) (*Tool, bool) {
	t, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	return cloneTool(t), true
}

func (r *Registry) List() []*Tool {
	out := make([]*Tool, 0, len(r.order))
	for _, n := range r.order {
		if t, ok := r.tools[n]; ok {
			out = append(out, cloneTool(t))
		}
	}
	return out
}

// cloneTool deep-copies the schema so callers cannot mutate registry state.
func cloneTool(t *Tool) *Tool {
	copied := *t
	if t.InputSchema != nil {
		copied.InputSchema = cloneMap(t.InputSchema)
	}
	return &copied
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch value := v.(type) {
		case map[string]any:
			out[k] = cloneMap(value)
		case []any:
			out[k] = cloneSlice(value)
		case []string:
			out[k] = append([]string(nil), value...)
		case map[string]string:
			copied := make(map[string]string, len(value))
			for mk, mv := range value {
				copied[mk] = mv
			}
			out[k] = copied
		default:
			out[k] = v
		}
	}
	return out
}

func cloneSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		switch value := v.(type) {
		case map[string]any:
			out[i] = cloneMap(value)
		case []any:
			out[i] = cloneSlice(value)
		default:
			out[i] = v
		}
	}
	return out
}

func (r *Registry) Names() []string {
	return append([]string(nil), r.order...)
}
