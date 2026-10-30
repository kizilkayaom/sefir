package email

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RecoverStaleOutboxEntries(
	ctx context.Context,
	pool *pgxpool.Pool,
	client *http.Client,
	claimedBefore time.Time,
	limit int,
) ([]OutboxEntry, error) {
	if pool == nil {
		return nil, errors.New("database pool is required")
	}

	if client == nil {
		return nil, errors.New("HTTP client is required")
	}
	return RecoverStaleOutboxEntriesWithClients(ctx, pool, func(context.Context, string) (*http.Client, error) {
		return client, nil
	}, claimedBefore, limit)
}

func RecoverStaleOutboxEntriesWithClients(ctx context.Context, pool *pgxpool.Pool,
	clientForOutbox func(context.Context, string) (*http.Client, error), claimedBefore time.Time, limit int,
) ([]OutboxEntry, error) {
	if pool == nil || clientForOutbox == nil {
		return nil, errors.New("database pool and Gmail client resolver are required")
	}
	entries, err := ListStaleOutboxEntries(ctx, pool, claimedBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("list stale outbox entries: %w", err)
	}

	var recoveryErrors []error

	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			recoveryErrors = append(recoveryErrors, err)
			break
		}

		if entry.RFC822MessageID == nil {
			recoveryErrors = append(recoveryErrors,
				fmt.Errorf("outbox %s is missing its Message-ID", entry.OutboxID))
			continue
		}

		client, err := clientForOutbox(ctx, entry.OutboxID)
		if err != nil || client == nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("Gmail connection unavailable for outbox %s", entry.OutboxID))
			continue
		}
		gmailMessageID, found, err := FindSentGmailMessage(ctx, client, *entry.RFC822MessageID)
		if err != nil {
			recoveryErrors = append(recoveryErrors,
				fmt.Errorf("lookup Gmail message for outbox %s: %w", entry.OutboxID, err))
			continue
		}
		if !found {
			continue
		}

		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		updated, err := MarkOutboxSent(saveCtx, pool, entry.OutboxID, gmailMessageID)
		cancel()
		if err != nil {
			recoveryErrors = append(recoveryErrors,
				fmt.Errorf("record recovered outbox %s as sent with Gmail message %s: %w", entry.OutboxID, gmailMessageID, err))
			continue
		}
		entries[i] = updated
	}

	return entries, errors.Join(recoveryErrors...)
}
