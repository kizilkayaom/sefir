package email

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

type DeliveryAccount struct {
	Client          *http.Client
	From            string
	MessageIDDomain string
}

type DeliveryAccountResolver func(context.Context, string) (DeliveryAccount, error)

func OutboxAccountID(ctx context.Context, pool *pgxpool.Pool, outboxID string) (string, error) {
	var accountID string
	err := pool.QueryRow(ctx, `SELECT c.account_id::text FROM outbox o
		JOIN approvals a ON a.approval_id=o.approval_id
		JOIN drafts d ON d.draft_id=a.draft_id AND d.version=a.version
		JOIN contacts c ON c.id=d.contact_id
		JOIN accounts acc ON acc.account_id=c.account_id
		WHERE o.outbox_id=$1 AND acc.deletion_requested_at IS NULL`, outboxID).Scan(&accountID)
	return accountID, err
}

func WorkspaceDeliveryAccounts(pool *pgxpool.Pool, root string) DeliveryAccountResolver {
	return func(ctx context.Context, outboxID string) (DeliveryAccount, error) {
		id, err := OutboxAccountID(ctx, pool, outboxID)
		if err != nil {
			return DeliveryAccount{}, err
		}
		path, err := workspace.GmailTokenPath(root, id)
		if err != nil {
			return DeliveryAccount{}, err
		}
		config, err := LoadOAuthConfig(filepath.Join(root, "credentials-gmail.json"), "http://localhost:8080/oauth/google/callback")
		if err != nil {
			return DeliveryAccount{}, errors.New("Gmail OAuth configuration is unavailable")
		}
		client, err := NewGmailClient(ctx, config, path)
		if err != nil {
			return DeliveryAccount{}, errors.New("connect Gmail for the account that owns this draft")
		}
		profile, err := GetGmailProfile(ctx, client)
		if err != nil {
			return DeliveryAccount{}, err
		}
		_, domain, ok := strings.Cut(profile.EmailAddress, "@")
		if !ok || domain == "" {
			return DeliveryAccount{}, errors.New("Gmail profile email is missing its domain")
		}
		return DeliveryAccount{Client: client, From: profile.EmailAddress, MessageIDDomain: domain}, nil
	}
}
