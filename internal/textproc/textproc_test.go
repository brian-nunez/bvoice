package textproc

import "testing"

func TestClean(t *testing.T) {
	cases := map[string]string{
		" Hello  world. ":               "Hello world.",
		"[BLANK_AUDIO]":                 "",
		" (music) So, [Music] we start": "So, we start",
		"*coughs* Right.":               "Right.",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJoin(t *testing.T) {
	got := Join([]string{" First part.", "[BLANK_AUDIO]", " Second part. "})
	if want := "First part. Second part."; got != want {
		t.Fatalf("Join = %q, want %q", got, want)
	}
}

func TestTail(t *testing.T) {
	if got := Tail("short", 200); got != "short" {
		t.Fatalf("Tail short = %q", got)
	}

	got := Tail("alpha beta gamma delta", 10)
	if got != "delta" && got != "gamma delta" {
		t.Fatalf("Tail should start on a word boundary, got %q", got)
	}
}

func TestPrompt(t *testing.T) {
	if got := Prompt("  Kubernetes,\n bvoice ", ""); got != "Kubernetes, bvoice" {
		t.Fatalf("vocab only = %q", got)
	}

	if got := Prompt("", "Previous text."); got != "Previous text." {
		t.Fatalf("previous only = %q", got)
	}

	if got := Prompt("PostgreSQL", "We migrated it."); got != "PostgreSQL We migrated it." {
		t.Fatalf("both = %q", got)
	}
}
