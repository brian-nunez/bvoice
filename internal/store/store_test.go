package store

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func TestSettingsPersist(t *testing.T) {
	s, dir := open(t)
	if err := s.SaveSettings(map[string]string{"sounds": "false", "vocab": "Jira"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(map[string]string{"sounds": "true"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(dir) // reopening must not re-run migrations or lose data
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	m, err := s2.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if m["sounds"] != "true" || m["vocab"] != "Jira" || len(m) != 2 {
		t.Fatalf("settings = %v", m)
	}
}

func TestHistoryLimitSearchClear(t *testing.T) {
	s, _ := open(t)
	base := time.Now().Add(-time.Hour)
	texts := []string{"first note", "Deploy to staging", "100% done_ok", "fourth note", "fifth NOTE"}
	for i, txt := range texts {
		e := Entry{CreatedAt: base.Add(time.Duration(i) * time.Minute), Model: "small.en-q5_1", AudioMs: 1000, LatencyMs: 900, Text: txt}
		if _, err := s.AddTranscription(e, 4); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.History("", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 || all[0].Text != "fifth NOTE" || all[3].Text != "Deploy to staging" {
		t.Fatalf("pruned/newest-first history wrong: %+v", all)
	}
	notes, _ := s.History("note", 100)
	if len(notes) != 2 {
		t.Fatalf("case-insensitive search found %d", len(notes))
	}
	if pct, _ := s.History("100%", 100); len(pct) != 1 {
		t.Fatalf("%% should be literal in search, got %d", len(pct))
	}
	if under, _ := s.History("e_o", 100); len(under) != 1 {
		t.Fatalf("_ should be literal in search, got %d", len(under))
	}
	got, err := s.Get(all[0].ID)
	if err != nil || got.Text != "fifth NOTE" || got.Model != "small.en-q5_1" {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	var md bytes.Buffer
	if err := s.ExportMarkdown(&md); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "# bvoice history") || strings.Index(md.String(), "Deploy") > strings.Index(md.String(), "fifth") {
		t.Fatalf("export wrong:\n%s", md.String())
	}

	if err := s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.History("", 100); len(left) != 0 {
		t.Fatalf("clear left %d entries", len(left))
	}
}
