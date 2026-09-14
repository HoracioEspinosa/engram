package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidSettingKey is returned for a key outside the dotted lowercase shape
// every setting takes. The shape is enforced rather than normalised on the way
// in: a caller that writes "TUI.theme" and reads "tui.theme" would otherwise
// get a silent miss instead of being told it named two different things.
var ErrInvalidSettingKey = errors.New("invalid setting key")

// settingKeyPattern is the shape of a setting key: lowercase segments joined by
// dots, each of which may contain hyphens and underscores. It is the namespace
// the prefix lookup slices on, so a key that ignores it also loses that.
var settingKeyPattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// maxSettingKeyLength mirrors the CHECK on the column.
const maxSettingKeyLength = 64

// Setting reads one setting. The boolean separates "set to the empty string"
// from "never set", which for a remembered choice are different answers.
func (s *Store) Setting(key string) (string, bool, error) {
	validKey, err := validSettingKey(key)
	if err != nil {
		return "", false, err
	}
	var value string
	err = s.queryRowHook(s.readDB(), `SELECT value FROM settings WHERE key = ?`, validKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("engram-projects: read setting %s: %w", validKey, err)
	}
	return value, true, nil
}

// Settings reads every setting under a prefix — "tui." for what the interface
// remembers — or all of them when the prefix is empty.
func (s *Store) Settings(prefix string) (map[string]string, error) {
	query := `SELECT key, value FROM settings ORDER BY key`
	var args []any
	if trimmed := strings.TrimSpace(prefix); trimmed != "" {
		// The prefix is matched with a range rather than with LIKE, so a key
		// containing an underscore — which LIKE reads as a wildcard — cannot
		// widen somebody else's namespace into the answer.
		query = `SELECT key, value FROM settings WHERE key >= ? AND key < ? ORDER BY key`
		args = []any{trimmed, prefixUpperBound(trimmed)}
	}

	rows, err := s.queryHook(s.readDB(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read settings: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("engram-projects: scan setting: %w", err)
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engram-projects: read settings: %w", err)
	}
	return out, nil
}

// SetSetting writes one setting, replacing whatever it held: a setting is the
// current answer, not a log of answers.
func (s *Store) SetSetting(key, value string) error {
	validKey, err := validSettingKey(key)
	if err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		validKey, value,
	); err != nil {
		return fmt.Errorf("engram-projects: write setting %s: %w", validKey, err)
	}
	return nil
}

// DeleteSetting removes a setting, so the next read falls back to the default.
// Deleting one that is already gone is the state the caller asked for, so it is
// not an error.
func (s *Store) DeleteSetting(key string) error {
	validKey, err := validSettingKey(key)
	if err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `DELETE FROM settings WHERE key = ?`, validKey); err != nil {
		return fmt.Errorf("engram-projects: delete setting %s: %w", validKey, err)
	}
	return nil
}

func validSettingKey(key string) (string, error) {
	if len(key) > maxSettingKeyLength || !settingKeyPattern.MatchString(key) {
		return "", fmt.Errorf("%w: %q (lowercase dotted segments, up to %d characters)",
			ErrInvalidSettingKey, key, maxSettingKeyLength)
	}
	return key, nil
}

// prefixUpperBound returns the first string that sorts after every string
// starting with prefix, so a prefix match becomes a range scan over the primary
// key instead of a pattern match over every row.
func prefixUpperBound(prefix string) string {
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return string(b[:i+1])
		}
	}
	// Every byte is 0xff, so nothing sorts after it; an empty bound would
	// select nothing, which is the honest answer for a prefix nothing exceeds.
	return prefix
}
