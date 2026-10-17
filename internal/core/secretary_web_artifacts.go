package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) SecretaryWebInput(ctx context.Context, personID, turnID, inputID string) (string, error) {
	if strings.TrimSpace(personID) == "" { return "", ErrNotFound }
	lookup := `SELECT t.input, t.input_id, t.input_entry_id
		FROM secretary_turns t JOIN secretary_identities i ON i.id = t.identity_id
		WHERE i.person_id = ? AND t.id = ? AND t.state = 'active'`
	if turnID == "" {
		lookup = `SELECT t.input, t.input_id, t.input_entry_id
			FROM secretary_turns t JOIN secretary_identities i ON i.id = t.identity_id
			WHERE i.person_id = ? AND t.state = 'active' ORDER BY t.started_at DESC LIMIT 1`
	}
	var originalInput, actualInputID, entryID string
	var err error
	if turnID == "" {
		err = s.db.QueryRowContext(ctx, lookup, personID).Scan(&originalInput, &actualInputID, &entryID)
	} else {
		err = s.db.QueryRowContext(ctx, lookup, personID, turnID).Scan(&originalInput, &actualInputID, &entryID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if turnID != "" { return "", ErrNotFound }
		return "", nil
	}
	if err != nil { return "", err }
	if inputID != "" && inputID != actualInputID { return "", ErrNotFound }
	if entryID == "" { return "", nil }
	var adapter, canonical string
	err = s.db.QueryRowContext(ctx, `SELECT im.adapter_id, ce.body FROM inbound_messages im
		JOIN conversation_entries ce ON ce.id = im.entry_id
		JOIN conversations c ON c.id = ce.conversation_id
		WHERE im.entry_id = ? AND c.person_id = ? AND ce.kind = 'user'`, entryID, personID).Scan(&adapter, &canonical)
	if errors.Is(err, sql.ErrNoRows) { return "", nil }
	if err != nil { return "", err }
	if adapter != "web" && !strings.HasPrefix(adapter, "client:") { return "", nil }
	if strings.TrimSpace(originalInput) == "" { return "", errors.New("core: Secretary input is empty") }
	return canonical, nil
}
