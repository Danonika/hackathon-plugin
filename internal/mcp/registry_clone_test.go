package mcp

import (
	"io"
	"testing"
)

// A typed container holding a nil interface element (e.g. map[string]error or
// []error) makes cloneValue return an untyped nil; reflecting that produced an
// invalid Value and panicked on assignment.
func TestCloneValueHandlesNilInterfaceElements(t *testing.T) {
	m := map[string]error{"missing": nil, "boom": io.EOF}
	clonedMap, ok := cloneValue(m).(map[string]error)
	if !ok {
		t.Fatalf("cloneValue(map[string]error) = %T", cloneValue(m))
	}
	if len(clonedMap) != 2 || clonedMap["missing"] != nil || clonedMap["boom"] != io.EOF {
		t.Fatalf("cloned map = %#v", clonedMap)
	}

	s := []error{nil, io.EOF}
	clonedSlice, ok := cloneValue(s).([]error)
	if !ok {
		t.Fatalf("cloneValue([]error) = %T", cloneValue(s))
	}
	if len(clonedSlice) != 2 || clonedSlice[0] != nil || clonedSlice[1] != io.EOF {
		t.Fatalf("cloned slice = %#v", clonedSlice)
	}
}
