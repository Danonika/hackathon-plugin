package resources

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestDefsAndTracks(t *testing.T) {
	if len(Defs()) != 6 {
		t.Fatalf("defs = %d, want 6", len(Defs()))
	}
	if len(Tracks()) != 10 {
		t.Fatalf("tracks = %d, want 10", len(Tracks()))
	}
	cases := map[string]string{
		"08":             "08-innovation",
		"08-innovation":  "08-innovation",
		"innovation":     "08-innovation",
		"Energy":         "01-energy",
		"01-energy":      "01-energy",
		"creative-media": "06-creative-media",
		"communications": "09-communications",
		"10":             "10-retail",
	}
	for ref, want := range cases {
		got, ok := LookupTrack(ref)
		if !ok || got.ID != want {
			t.Fatalf("LookupTrack(%q) = %+v, %v; want %s", ref, got, ok, want)
		}
	}
	if _, ok := LookupTrack("99-unknown"); ok {
		t.Fatal("unknown track must not resolve")
	}
	if _, ok := LookupTrack(""); ok {
		t.Fatal("empty track must not resolve")
	}
}

func TestStatusesMergeDetectionAndAnswers(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("TELEGRAM_CHAT_ID", "123")
	os.Unsetenv("NVIDIA_API_KEY")

	profile := Profile{Claimed: map[string]bool{
		"openai_api": true,
		"nvidia_api": true,
		"codex_pro":  true,
	}}
	statuses := Statuses(profile)
	byID := map[string]Status{}
	for _, s := range statuses {
		byID[s.ID] = s
	}

	if !byID["openai_api"].Detected || !byID["openai_api"].Ready {
		t.Fatalf("openai_api = %+v, want detected+ready", byID["openai_api"])
	}
	nvidia := byID["nvidia_api"]
	if nvidia.Detected || nvidia.Ready || !nvidia.Claimed {
		t.Fatalf("nvidia_api = %+v, want claimed but not ready", nvidia)
	}
	if len(nvidia.MissingKeys) != 1 || nvidia.MissingKeys[0] != "NVIDIA_API_KEY" {
		t.Fatalf("nvidia missing keys = %v", nvidia.MissingKeys)
	}
	if !strings.Contains(nvidia.Hint, "env-set.sh .env NVIDIA_API_KEY") {
		t.Fatalf("nvidia hint = %q", nvidia.Hint)
	}
	if !byID["telegram"].Ready {
		t.Fatalf("telegram = %+v, want ready", byID["telegram"])
	}
	if !byID["codex_pro"].Ready || !byID["codex_pro"].Manual {
		t.Fatalf("codex_pro = %+v, want manual+ready", byID["codex_pro"])
	}
	if byID["postgres"].Ready || byID["postgres"].Claimed {
		t.Fatalf("postgres = %+v, want unclaimed and not ready", byID["postgres"])
	}

	summary := Summary(statuses)
	if !strings.Contains(summary, "openai_api=yes") || !strings.Contains(summary, "nvidia_api=claimed") {
		t.Fatalf("summary = %q", summary)
	}
	if missing := Missing(statuses); len(missing) != 1 || missing[0] != "NVIDIA_API_KEY" {
		t.Fatalf("missing = %v", missing)
	}
	if !Ready(statuses, "telegram") || Ready(statuses, "minio") {
		t.Fatal("Ready mismatch")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	root := t.TempDir()
	profile := Profile{Track: "08", Note: "hackathon", CodexPro: true}
	if err := SetAnswer(&profile, "openai_api", true); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, profile); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := Load(root)
	if err != nil || !ok {
		t.Fatalf("Load = %+v, %v, %v", loaded, ok, err)
	}
	if loaded.Track != "08-innovation" {
		t.Fatalf("track = %q, want 08-innovation", loaded.Track)
	}
	if loaded.Version != ProfileVersion || loaded.UpdatedAt.IsZero() {
		t.Fatalf("profile = %+v", loaded)
	}
	if !loaded.Claimed["openai_api"] || !loaded.CodexPro {
		t.Fatalf("claimed = %+v codex_pro=%v", loaded.Claimed, loaded.CodexPro)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(Path(root))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("profile mode = %v, want 0600", info.Mode().Perm())
		}
	}

	if _, ok, err := Load(t.TempDir()); err != nil || ok {
		t.Fatalf("missing profile: ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(Path(root), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root); err == nil {
		t.Fatal("corrupt profile must fail")
	}
}

func TestSaveRejectsUnknownTrackAndProvider(t *testing.T) {
	if err := Save(t.TempDir(), Profile{Track: "99-unknown"}); err == nil {
		t.Fatal("unknown track must fail")
	}
	var profile Profile
	if err := SetAnswer(&profile, "gemini", true); err == nil {
		t.Fatal("unknown provider must fail")
	}
}
