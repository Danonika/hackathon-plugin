package mcp

import (
	"context"
	"log"
	"reflect"
	"sync"
)

type Handler func(ctx context.Context, args map[string]any) (any, error)

type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     Handler
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]*Tool
	order []string
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]*Tool{}}
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		// A zero-value Registry is usable; initialize the map lazily so
		// Register cannot panic with "assignment to entry in nil map".
		r.tools = map[string]*Tool{}
	}
	if _, exists := r.tools[t.Name]; exists {
		log.Printf("mcp: duplicate tool registration for %q; overwriting", t.Name)
	} else {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = cloneTool(&t)
}

func (r *Registry) Get(name string) (*Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	return cloneTool(t), true
}

func (r *Registry) List() []*Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
		out[k] = cloneValue(v)
	}
	return out
}

func cloneSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = cloneValue(v)
	}
	return out
}

// cloneValue deep-copies the JSON-like containers used by input schemas,
// including typed slices, maps and nested combinations. Pointers, structs and
// arrays fall through unchanged (schemas are built from JSON-like values only),
// so callers must not rely on those being detached.
func cloneValue(v any) any {
	switch value := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneMap(value)
	case []any:
		return cloneSlice(value)
	case []string:
		return append([]string(nil), value...)
	case map[string]string:
		copied := make(map[string]string, len(value))
		for mk, mv := range value {
			copied[mk] = mv
		}
		return copied
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		for _, key := range rv.MapKeys() {
			// cloneValue returns an untyped nil for a nil interface element,
			// and reflect.ValueOf(nil) is the invalid zero Value, which
			// SetMapIndex panics on. Substitute a typed zero instead.
			cloned := reflect.ValueOf(cloneValue(rv.MapIndex(key).Interface()))
			if !cloned.IsValid() {
				cloned = reflect.Zero(rv.Type().Elem())
			}
			out.SetMapIndex(key, cloned)
		}
		return out.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			cloned := reflect.ValueOf(cloneValue(rv.Index(i).Interface()))
			if !cloned.IsValid() {
				cloned = reflect.Zero(rv.Type().Elem())
			}
			out.Index(i).Set(cloned)
		}
		return out.Interface()
	default:
		return v
	}
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}
