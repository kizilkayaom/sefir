package webapp

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type AccountDeletionStore interface {
	PendingDeletionPage(context.Context, string, int) ([]string, error)
	SettleDeletingOutbox(context.Context, string) (bool, error)
	PurgeAccount(context.Context, string) (bool, error)
}

type AccountDeletionWorker struct {
	Accounts AccountDeletionStore
	Cleaner  AccountCleaner
}

func (w AccountDeletionWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	afterID := ""
	for ctx.Err() == nil {
		next, err := w.processBatch(ctx, afterID)
		if err != nil && ctx.Err() == nil {
			slog.Error("Account cleanup will retry", "error", err)
		}
		afterID = next
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w AccountDeletionWorker) processBatch(ctx context.Context, afterID string) (string, error) {
	if w.Accounts == nil || w.Cleaner.Accounts == nil || w.Cleaner.Bridge == nil || w.Cleaner.RemoveGmailToken == nil {
		return afterID, errors.New("account cleanup is not configured")
	}
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	ids, err := w.Accounts.PendingDeletionPage(listCtx, afterID, 10)
	cancel()
	if err != nil {
		return afterID, err
	}
	for _, accountID := range ids {
		if ctx.Err() != nil {
			return afterID, ctx.Err()
		}
		accountCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		deleted, err := w.deleteAccount(accountCtx, accountID)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Error("Account deletion will retry", "account_id", accountID, "error", err)
		} else if deleted {
			slog.Info("Account deletion completed", "account_id", accountID)
		}
		afterID = accountID
	}
	if len(ids) < 10 {
		afterID = ""
	}
	return afterID, nil
}

func (w AccountDeletionWorker) deleteAccount(ctx context.Context, accountID string) (bool, error) {
	ready, err := w.Accounts.SettleDeletingOutbox(ctx, accountID)
	if err != nil || !ready {
		return false, err
	}
	ready, err = w.Cleaner.prepareDeletion(ctx, accountID)
	if err != nil || !ready {
		return false, err
	}
	return w.Accounts.PurgeAccount(ctx, accountID)
}
