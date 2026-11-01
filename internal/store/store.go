// Package store keeps settings and transcription history in SQLite.
// It uses github.com/mattn/go-sqlite3, which compiles SQLite itself into the single binary.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // registers the "sqlite3" database/sql driver

	"github.com/brian-nunez/bvoice/internal/history"
)

// Entry is a saved transcription.
type Entry = history.Entry

// Store is the bvoice database.
type Store struct {
	db *sql.DB
}

// DefaultDir is ~/Library/Application Support/bvoice on macOS (os.UserConfigDir elsewhere).
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(base, "bvoice"), nil
}

var migrations = []string{
	`CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE transcriptions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at INTEGER NOT NULL,
		model TEXT NOT NULL,
		audio_ms INTEGER NOT NULL,
		latency_ms INTEGER NOT NULL,
		text TEXT NOT NULL
	);
	CREATE INDEX transcriptions_created ON transcriptions(created_at DESC);`,
}

// Open creates the data folder if needed, opens <dir>/bvoice.db and applies migrations.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data folder: %w", err)
	}
	// busy_timeout lets several bvoice binaries share the database.
	dsn := "file:" + filepath.Join(dir, "bvoice.db") + "?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=1"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, errors.Join(fmt.Errorf("migrate database: %w", err), db.Close())
	}

	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}

	var v int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v); err != nil {
		return err
	}

	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}

		if _, err := tx.Exec(migrations[i]); err != nil {
			return errors.Join(err, tx.Rollback())
		}

		if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
			return errors.Join(err, tx.Rollback())
		}

		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, i+1); err != nil {
			return errors.Join(err, tx.Rollback())
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// LoadSettings returns all saved settings as key → value.
func (s *Store) LoadSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	m := map[string]string{}

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}

		m[k] = v
	}

	return m, rows.Err()
}

// SaveSettings upserts the given settings.
func (s *Store) SaveSettings(m map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	for k, v := range m {
		if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, k, v, now); err != nil {
			return errors.Join(err, tx.Rollback())
		}
	}

	return tx.Commit()
}

// AddTranscription saves an entry and prunes history to keep at most limit entries.
func (s *Store) AddTranscription(e Entry, limit int) (int64, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}

	res, err := s.db.Exec(`INSERT INTO transcriptions (created_at, model, audio_ms, latency_ms, text) VALUES (?, ?, ?, ?, ?)`,
		e.CreatedAt.UnixMilli(), e.Model, e.AudioMs, e.LatencyMs, e.Text)
	if err != nil {
		return 0, err
	}

	id, _ := res.LastInsertId()

	return id, s.Prune(limit)
}

// Prune deletes all but the newest limit entries.
func (s *Store) Prune(limit int) error {
	if limit < 1 {
		limit = 1
	}

	_, err := s.db.Exec(`DELETE FROM transcriptions WHERE id NOT IN
		(SELECT id FROM transcriptions ORDER BY created_at DESC, id DESC LIMIT ?)`, limit)

	return err
}

// History returns up to limit entries, newest first, whose text contains query (case-insensitive).
func (s *Store) History(query string, limit int) ([]Entry, error) {
	q := `SELECT id, created_at, model, audio_ms, latency_ms, text FROM transcriptions`
	args := []any{}

	if query = strings.TrimSpace(query); query != "" {
		q += ` WHERE text LIKE ? ESCAPE '\'` // LIKE is case-insensitive for ASCII

		args = append(args, "%"+escapeLike(query)+"%")
	}

	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`

	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Entry

	for rows.Next() {
		var (
			e       Entry
			created int64
		)
		if err := rows.Scan(&e.ID, &created, &e.Model, &e.AudioMs, &e.LatencyMs, &e.Text); err != nil {
			return nil, err
		}

		e.CreatedAt = time.UnixMilli(created)
		out = append(out, e)
	}

	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Get returns one entry.
func (s *Store) Get(id int64) (Entry, error) {
	var (
		e       Entry
		created int64
	)

	err := s.db.QueryRow(`SELECT id, created_at, model, audio_ms, latency_ms, text FROM transcriptions WHERE id = ?`, id).
		Scan(&e.ID, &created, &e.Model, &e.AudioMs, &e.LatencyMs, &e.Text)
	e.CreatedAt = time.UnixMilli(created)

	return e, err
}

// ClearHistory deletes every transcription.
func (s *Store) ClearHistory() error {
	_, err := s.db.Exec(`DELETE FROM transcriptions`)

	return err
}

// ExportMarkdown writes the whole history, oldest first, as Markdown.
func (s *Store) ExportMarkdown(w io.Writer) error {
	rows, err := s.db.Query(`SELECT created_at, model, audio_ms, text FROM transcriptions ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	if _, err := fmt.Fprintf(w, "# bvoice history\n\nExported %s\n", time.Now().Format("2006-01-02 15:04")); err != nil {
		return err
	}

	for rows.Next() {
		var (
			created, audio int64
			model, text    string
		)
		if err := rows.Scan(&created, &model, &audio, &text); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, "\n## %s · %s · %ds\n\n%s\n",
			time.UnixMilli(created).Format("2006-01-02 15:04:05"), model, audio/1000, text); err != nil {
			return err
		}
	}

	return rows.Err()
}
