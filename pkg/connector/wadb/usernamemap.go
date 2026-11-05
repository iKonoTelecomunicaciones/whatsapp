package wadb

import (
	"context"
	"database/sql"

	"github.com/iKonoTelecomunicaciones/whatsmeow/types"
	"go.mau.fi/util/dbutil"
)

type UsernameMapQuery struct {
	*dbutil.QueryHelper[*UsernameMapEntry]
}

const (
	getUsernameMapEntryByUsername = `
		SELECT lid, pn, username
		FROM whatsapp_username_map
		WHERE username = $1
	`
	getUsernameMapEntryByLID = `
		SELECT lid, pn, username
		FROM whatsapp_username_map
		WHERE lid = $1
	`
	getUsernameMapEntryByPN = `
		SELECT lid, pn, username
		FROM whatsapp_username_map
		WHERE pn = $1
	`
	putUsernameMapEntry = `
		INSERT INTO whatsapp_username_map (lid, pn, username)
		VALUES ($1, $2, $3)
		ON CONFLICT (lid) DO UPDATE
		SET pn = COALESCE(EXCLUDED.pn, whatsapp_username_map.pn),
		    username = COALESCE(EXCLUDED.username, whatsapp_username_map.username)
	`
)

func (umq *UsernameMapQuery) GetByUsername(ctx context.Context, username string) (*UsernameMapEntry, error) {
	return umq.QueryOne(ctx, getUsernameMapEntryByUsername, username)
}

func (umq *UsernameMapQuery) GetByLID(ctx context.Context, lid types.JID) (*UsernameMapEntry, error) {
	return umq.QueryOne(ctx, getUsernameMapEntryByLID, lid)
}

func (umq *UsernameMapQuery) GetByPN(ctx context.Context, pn types.JID) (*UsernameMapEntry, error) {
	return umq.QueryOne(ctx, getUsernameMapEntryByPN, pn)
}

func (umq *UsernameMapQuery) Put(ctx context.Context, entry *UsernameMapEntry) error {
	return umq.Exec(ctx, putUsernameMapEntry, entry.sqlVariables()...)
}

type UsernameMapEntry struct {
	LID      types.JID
	PN       types.JID
	Username string
}

func (ume *UsernameMapEntry) Scan(row dbutil.Scannable) (*UsernameMapEntry, error) {
	var username sql.NullString
	err := row.Scan(&ume.LID, &ume.PN, &username)
	if err == nil {
		ume.Username = username.String
	}
	return dbutil.ValueOrErr(ume, err)
}

func (ume *UsernameMapEntry) sqlVariables() []any {
	var username any
	if ume.Username != "" {
		username = ume.Username
	}
	return []any{ume.LID, ume.PN, username}
}
