package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSettleDeletingOutboxWaitsAndKeepsOtherAccounts(t *testing.T) {
	pool := accountPurgeTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	target := seedPurgeAccount(t, ctx, pool, "target", 1, "sending")
	other := seedPurgeAccount(t, ctx, pool, "other", 2, "sending")
	if ready, err := accounts.SettleDeletingOutbox(ctx, target.accountID); err != nil || ready {
		t.Fatalf("did not wait for in-flight requests: %v %v", ready, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET deletion_requested_at=now()-interval '3 minutes' WHERE account_id=$1`, target.accountID); err != nil {
		t.Fatal(err)
	}
	if ready, err := accounts.SettleDeletingOutbox(ctx, target.accountID); err != nil || !ready {
		t.Fatalf("ready: %v %v", ready, err)
	}
	if sending, err := accounts.HasSendingOutbox(ctx, target.accountID); err != nil || !sending {
		t.Fatalf("recent claim was closed: %v %v", sending, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox SET claimed_at=now()-interval '3 minutes'`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if ready, err := accounts.SettleDeletingOutbox(ctx, target.accountID); err != nil || !ready {
			t.Fatalf("ready: %v %v", ready, err)
		}
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT state, attempts FROM outbox WHERE outbox_id=$1", target.outboxID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "needs_attention" || attempts != 0 {
		t.Fatalf("delivery was retried or marked successful: %s %d", state, attempts)
	}
	if sending, err := accounts.HasSendingOutbox(ctx, other.accountID); err != nil || !sending {
		t.Fatalf("other account changed: %v %v", sending, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE accounts SET deletion_requested_at=NULL WHERE account_id=$1", other.accountID); err != nil {
		t.Fatal(err)
	}
	if ready, err := accounts.SettleDeletingOutbox(ctx, other.accountID); err != nil || ready {
		t.Fatalf("active account accepted: %v %v", ready, err)
	}
}

func TestAccountDeletionWorkerPurgesOnlyAfterHistoryRemoval(t *testing.T) {
	pool := accountPurgeTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	target := seedPurgeAccount(t, ctx, pool, "target", 1, "pending")
	other := seedPurgeAccount(t, ctx, pool, "other", 2, "pending")
	if _, err := pool.Exec(ctx, `UPDATE accounts SET deletion_requested_at=now()-interval '3 minutes' WHERE account_id=$1`, target.accountID); err != nil {
		t.Fatal(err)
	}
	historyGone := false
	requests := 0
	workflowID := "approval-" + target.draftID + "-v1"
	bridge := cleanupBridgeFunc(func(_ context.Context, action string, payload any) (json.RawMessage, error) {
		input := payload.(map[string]string)
		if input["workflow_id"] != workflowID {
			t.Fatal("touched another account's workflow")
		}
		response := map[string]any{"workflow_id": workflowID, "run_id": "run-one"}
		switch action {
		case "stop_approval":
			response["stopped"] = true
		case "list_approval_runs":
			response["run_ids"] = []string{}
			if !historyGone {
				response["run_ids"] = []string{"run-one"}
			}
		case "approval_run_deleted":
			response["deleted"] = historyGone
		case "request_approval_run_deletion":
			requests++
			response["deletion_requested"] = true
		default:
			t.Fatalf("unexpected action: %s", action)
		}
		return json.Marshal(response)
	})
	newWorker := func() AccountDeletionWorker {
		return AccountDeletionWorker{Accounts: accounts, Cleaner: AccountCleaner{
			Accounts: accounts, Bridge: bridge, RemoveGmailToken: func(id string) error {
				if id != target.accountID {
					t.Fatal("removed another account's token")
				}
				return nil
			},
		}}
	}
	if done, err := newWorker().deleteAccount(ctx, target.accountID); err != nil || done {
		t.Fatalf("premature purge: %v %v", done, err)
	}
	assertPurgeAccountRows(t, ctx, pool, target, true)
	runs, err := accounts.DeletionRuns(ctx, target.accountID)
	if err != nil || len(runs) != 1 || requests != 1 {
		t.Fatalf("tracking missing: %v %v", runs, err)
	}
	historyGone = true
	if done, err := newWorker().deleteAccount(ctx, target.accountID); err != nil || !done {
		t.Fatalf("restart cleanup: %v %v", done, err)
	}
	assertPurgeAccountRows(t, ctx, pool, target, false)
	assertPurgeAccountRows(t, ctx, pool, other, true)
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM account_deletion_runs WHERE account_id=$1", target.accountID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("tracking not purged: %d %v", count, err)
	}
}

func TestPendingDeletionPageDoesNotStarveLaterAccounts(t *testing.T) {
	pool := accountTestPool(t)
	ctx := context.Background()
	accounts := PostgresAccounts{Pool: pool}
	want := make([]string, 0, 12)
	for i := range 12 {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO accounts (username, deletion_requested_at) VALUES ($1, now()) RETURNING account_id::text`, fmt.Sprintf("delete-%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO accounts (username) VALUES ('active')"); err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	first, err := accounts.PendingDeletionPage(ctx, "", 10)
	if err != nil || !slices.Equal(first, want[:10]) {
		t.Fatalf("first page: %v %v", first, err)
	}
	last, err := accounts.PendingDeletionPage(ctx, first[9], 10)
	if err != nil || !slices.Equal(last, want[10:]) {
		t.Fatalf("last page: %v %v", last, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := accounts.PendingDeletionPage(ctx, "", limit); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if _, err := accounts.PendingDeletionPage(ctx, "not-an-id", 10); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestWithActiveAccountSerializesDeletion(t *testing.T) {
	admin := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schema := fmt.Sprintf("deletion_lock_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if _, err := admin.Exec(ctx, "CREATE TABLE "+quoted+".accounts (LIKE public.accounts INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	config := admin.Config()
	config.MaxConns = 3
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	accounts := PostgresAccounts{Pool: pool}
	problem := errors.New("bridge unavailable")
	for i, result := range []error{nil, problem} {
		var id string
		if err := pool.QueryRow(ctx, "INSERT INTO accounts (username) VALUES ($1) RETURNING account_id::text", fmt.Sprintf("active-%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		err := accounts.WithActiveAccount(ctx, id, func() error {
			waiting, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			if err := accounts.RequestDeletion(waiting, id); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deletion did not wait for approval: %v", err)
			}
			return result
		})
		if !errors.Is(err, result) {
			t.Fatalf("callback result: %v", err)
		}
		if err := accounts.RequestDeletion(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := accounts.WithActiveAccount(ctx, id, func() error { t.Fatal("started after deletion"); return nil }); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("pending account accepted: %v", err)
		}
	}
}
