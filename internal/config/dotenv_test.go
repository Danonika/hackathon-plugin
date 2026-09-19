package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	root := t.TempDir()
	content := "# comment line\nOPENAI_API_KEY=test-key-123\nEXISTING=from-file\nQUOTED=\"quoted value\"\n\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("QUOTED")
	os.Setenv("EXISTING", "from-env")
	defer os.Unsetenv("OPENAI_API_KEY")
	defer os.Unsetenv("QUOTED")
	defer os.Unsetenv("EXISTING")

	LoadDotEnv(root)

	if got := os.Getenv("OPENAI_API_KEY"); got != "test-key-123" {
		t.Fatalf("expected key from .env, got %q", got)
	}
	if got := os.Getenv("EXISTING"); got != "from-env" {
		t.Fatalf("environment must win over .env, got %q", got)
	}
	if got := os.Getenv("QUOTED"); got != "quoted value" {
		t.Fatalf("expected quotes to be trimmed, got %q", got)
	}

	LoadDotEnv(filepath.Join(root, "missing"))
}
