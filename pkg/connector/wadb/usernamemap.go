package wadb

import (
	"context"

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
		SET pn = EXCLUDED.pn, username = EXCLUDED.username
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
	return dbutil.ValueOrErr(ume, row.Scan(&ume.LID, &ume.PN, &ume.Username))
}

func (ume *UsernameMapEntry) sqlVariables() []any {
	return []any{ume.LID, ume.PN, ume.Username}
}
