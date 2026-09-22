package skills

import (
	"path/filepath"
	"testing"
)

// TestCatalogPassesStandard keeps the real skill catalog compliant with
// docs/SKILL-STANDARD.md: any future edit that breaks a rule fails here.
func TestCatalogPassesStandard(t *testing.T) {
	report, err := Validate(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total < 16 {
		t.Fatalf("expected at least 16 skills, found %d", report.Summary.Total)
	}
	for _, item := range report.Skills {
		if !item.OK {
			t.Errorf("%s (%d lines): %v", item.Name, item.Lines, item.Problems)
		}
	}
}
