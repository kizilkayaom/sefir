package email

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func outboxStateForSendError(err error) string {
	if errors.Is(err, ErrGmailReconnectRequired) {
		return "failed"
	}

	var apiErr *GmailAPIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusBadRequest ||
			apiErr.StatusCode == http.StatusForbidden {
			return "failed"
		}
	}

	return "needs_attention"
}

func SendOutboxMessage(
	ctx context.Context,
	pool *pgxpool.Pool,
	client *http.Client,
	outboxID string,
	from string,
	messageIDDomain string,
) (OutboxEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if pool == nil || client == nil {
		return OutboxEntry{}, errors.New("database pool and HTTP client are required")
	}
	if _, err := parseMailbox(from); err != nil {
		return OutboxEntry{}, fmt.Errorf("invalid sender: %w", err)
	}

	entry, claimed, err := ClaimOutbox(ctx, pool, outboxID, messageIDDomain)
	if err != nil {
		return OutboxEntry{}, fmt.Errorf("claim outbox: %w", err)
	}
	if !claimed {
		return entry, nil
	}

	recordProblem := func(state string, cause error) (OutboxEntry, error) {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		updated, saveErr := MarkOutboxProblem(saveCtx, pool, entry.OutboxID, state, cause.Error())
		if saveErr != nil {
			return entry, errors.Join(cause, fmt.Errorf("record outbox problem: %w", saveErr))
		}
		return updated, nil
	}

	draft, err := GetApprovedDraft(ctx, pool, entry.OutboxID)
	if err != nil {
		return recordProblem("needs_attention", fmt.Errorf("load approved draft: %w", err))
	}
	if entry.RFC822MessageID == nil {
		return recordProblem("needs_attention", errors.New("claimed outbox entry is missing its Message-ID"))
	}
	if entry.ClaimedAt == nil {
		return recordProblem("needs_attention", errors.New("claimed outbox entry is missing its claim time"))
	}

	message, err := BuildMessage(MessageInput{
		From:      from,
		Draft:     draft,
		MessageID: *entry.RFC822MessageID,
		Date:      *entry.ClaimedAt,
	})
	if err != nil {
		return recordProblem("needs_attention", fmt.Errorf("build message: %w", err))
	}

	result, err := SendGmailMessage(ctx, client, message)
	if err != nil {
		return recordProblem(outboxStateForSendError(err), fmt.Errorf("send Gmail message: %w", err))
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	updated, err := MarkOutboxSent(saveCtx, pool, entry.OutboxID, result.ID)
	if err != nil {
		return entry, fmt.Errorf("Gmail accepted message %s, but recording it as sent failed: %w", result.ID, err)
	}
	return updated, nil
}
