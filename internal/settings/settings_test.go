package settings

import (
	"flag"
	"io"
	"testing"
)

func parse(t *testing.T, args ...string) (Overrides, error) {
	t.Helper()

	fs := flag.NewFlagSet("bvoice", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	RegisterFlags(fs)

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return Collect(fs)
}

func TestRoundTrip(t *testing.T) {
	s := Defaults()
	s.Vocabulary = "Kubernetes, PostgreSQL"
	s.SoundVolume = 0.25
	s.Hotkey = "right_command"

	got, warnings := FromMap(s.ToMap())
	if len(warnings) != 0 || got != s {
		t.Fatalf("round trip mismatch: %+v vs %+v (%v)", got, s, warnings)
	}
}

func TestFromMapBadValuesFallBack(t *testing.T) {
	got, warnings := FromMap(map[string]string{"sounds": "maybe", "history-limit": "-4", "unknown": "x", "auto-copy": "false"})
	if len(warnings) != 2 {
		t.Fatalf("want 2 warnings, got %v", warnings)
	}

	d := Defaults()
	if got.Sounds != d.Sounds || got.HistoryLimit != d.HistoryLimit || got.AutoCopy {
		t.Fatalf("unexpected settings %+v", got)
	}
}

func TestFlagsOverrideForThisRunAndDiff(t *testing.T) {
	saved := Defaults()
	saved.Sounds = false

	o, err := parse(t, "--sounds", "--max-minutes=45", "--hotkey", "left_option")
	if err != nil {
		t.Fatal(err)
	}

	eff := Apply(saved, o)
	if !eff.Sounds || eff.MaxMinutes != 45 || saved.Sounds {
		t.Fatalf("apply wrong: eff=%+v saved=%+v", eff, saved)
	}

	diff := Diff(saved, o)
	// hotkey equals the saved value, so only sounds and max-minutes differ, in field order
	if len(diff) != 2 || diff[0].Key != "sounds" || diff[1].Key != "max-minutes" {
		t.Fatalf("diff = %+v", diff)
	}

	if diff[0].Saved != "false" || diff[0].Flag != "true" {
		t.Fatalf("diff values = %+v", diff[0])
	}
}

func TestInvalidFlagRejected(t *testing.T) {
	for _, args := range [][]string{
		{"--hotkey=space"}, {"--sound-volume=2"}, {"--auto-stop=abc"}, {"--sounds=perhaps"},
	} {
		if _, err := parse(t, args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestEveryFieldHasAFlagAndDefault(t *testing.T) {
	d := Defaults()

	seen := map[string]bool{}
	for _, f := range Fields {
		if seen[f.Key] {
			t.Fatalf("duplicate key %s", f.Key)
		}

		seen[f.Key] = true
		if err := d.Set(f.Key, f.get(&d)); err != nil {
			t.Fatalf("default for %s does not validate: %v", f.Key, err)
		}
	}
}
