package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type fakeDeletionWorkerStore struct {
	page   func(context.Context, string, int) ([]string, error)
	settle func(context.Context, string) (bool, error)
	purge  func(context.Context, string) (bool, error)
}

func (f fakeDeletionWorkerStore) PendingDeletionPage(ctx context.Context, after string, limit int) ([]string, error) {
	return f.page(ctx, after, limit)
}

func (f fakeDeletionWorkerStore) SettleDeletingOutbox(ctx context.Context, id string) (bool, error) {
	return f.settle(ctx, id)
}

func (f fakeDeletionWorkerStore) PurgeAccount(ctx context.Context, id string) (bool, error) {
	return f.purge(ctx, id)
}

func TestAccountDeletionWorkerResumesAfterRestart(t *testing.T) {
	ctx := context.Background()
	var saved []DeletionRun
	workflowID := "approval-" + testID + "-v1"
	historyGone := false
	purged := false
	removeFails := true
	purgeFails := true
	requests := 0
	store := &fakeAccountCleanupStore{
		sending:   func(context.Context, string) (bool, error) { return false, nil },
		workflows: func(context.Context, string) ([]string, error) { return []string{workflowID}, nil },
		saveRun: func(_ context.Context, accountID, workflowID, runID string) error {
			if accountID != testID {
				t.Fatal("wrong account")
			}
			saved = []DeletionRun{{WorkflowID: workflowID, RunID: runID}}
			return nil
		},
		runs: func(context.Context, string) ([]DeletionRun, error) { return saved, nil },
	}
	bridge := cleanupBridgeFunc(func(_ context.Context, action string, payload any) (json.RawMessage, error) {
		input := payload.(map[string]string)
		response := map[string]any{"workflow_id": input["workflow_id"], "run_id": input["run_id"]}
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
			t.Fatalf("unexpected operation: %s", action)
		}
		return json.Marshal(response)
	})
	newWorker := func() AccountDeletionWorker {
		return AccountDeletionWorker{
			Accounts: fakeDeletionWorkerStore{
				settle: func(context.Context, string) (bool, error) { return true, nil },
				purge: func(context.Context, string) (bool, error) {
					if !historyGone || removeFails {
						t.Fatal("purged before cleanup completed")
					}
					if purgeFails {
						return false, errors.New("temporary database failure")
					}
					purged = true
					return true, nil
				},
			},
			Cleaner: AccountCleaner{Accounts: store, Bridge: bridge, RemoveGmailToken: func(string) error {
				if removeFails {
					return errors.New("temporary filesystem failure")
				}
				return nil
			}},
		}
	}
	if done, err := newWorker().deleteAccount(ctx, testID); err == nil || done || purged {
		t.Fatalf("filesystem failure: %v %v", done, err)
	}
	removeFails = false
	if done, err := newWorker().deleteAccount(ctx, testID); err != nil || done || purged || requests != 1 || len(saved) != 1 {
		t.Fatalf("pending history deletion: %v %v; requests=%d runs=%v", done, err, requests, saved)
	}
	historyGone = true
	if done, err := newWorker().deleteAccount(ctx, testID); err == nil || done || purged {
		t.Fatalf("purge failure: %v %v", done, err)
	}
	purgeFails = false
	if done, err := newWorker().deleteAccount(ctx, testID); err != nil || !done || !purged || requests != 1 {
		t.Fatalf("restart did not finish persisted cleanup: %v %v", done, err)
	}
}

func TestAccountDeletionWorkerWaitsAndPropagatesErrors(t *testing.T) {
	problem := errors.New("database unavailable")
	for _, tc := range []struct {
		name  string
		ready bool
		err   error
	}{
		{name: "grace period"},
		{name: "database failure", err: problem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := AccountDeletionWorker{Accounts: fakeDeletionWorkerStore{
				settle: func(context.Context, string) (bool, error) { return tc.ready, tc.err },
			}}
			done, err := worker.deleteAccount(context.Background(), testID)
			if done || !errors.Is(err, tc.err) {
				t.Fatalf("got %v %v", done, err)
			}
		})
	}
}

func TestAccountDeletionWorkerContinuesPastFailuresAndCyclesPages(t *testing.T) {
	var attempts []string
	worker := AccountDeletionWorker{
		Cleaner: AccountCleaner{Accounts: &fakeAccountCleanupStore{}, Bridge: &fakeBridge{}, RemoveGmailToken: func(string) error { return nil }},
		Accounts: fakeDeletionWorkerStore{
			page: func(_ context.Context, after string, limit int) ([]string, error) {
				if limit != 10 {
					t.Fatalf("unexpected batch size %d", limit)
				}
				if after == "j" {
					return nil, nil
				}
				if after != "" {
					t.Fatalf("unexpected cursor %q", after)
				}
				return []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, nil
			},
			settle: func(_ context.Context, id string) (bool, error) {
				attempts = append(attempts, id)
				if id == "a" {
					return false, errors.New("temporary failure")
				}
				return false, nil
			},
		},
	}
	next, err := worker.processBatch(context.Background(), "")
	if err != nil || next != "j" || !reflect.DeepEqual(attempts, []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}) {
		t.Fatalf("failed to move past blocked accounts: %q %v %v", next, err, attempts)
	}
	next, err = worker.processBatch(context.Background(), next)
	if err != nil || next != "" {
		t.Fatalf("failed to cycle: %q %v", next, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Run(ctx)
	if len(attempts) != 10 {
		t.Fatal("cancelled worker processed accounts")
	}
}
