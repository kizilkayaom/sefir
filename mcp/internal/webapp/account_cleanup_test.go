package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func TestStopApprovalWorkflow(t *testing.T) {
	workflowID := "approval-" + testID + "-v1"
	success := `{"workflow_id":"` + workflowID + `","stopped":true}`
	bridgeError := errors.New("bridge unavailable")
	for _, tc := range []struct {
		name      string
		response  string
		bridgeErr error
		wantErr   bool
	}{
		{name: "confirmed", response: success},
		{name: "bridge error", response: success, bridgeErr: bridgeError, wantErr: true},
		{name: "invalid JSON", response: `{`, wantErr: true},
		{name: "wrong confirmation type", response: `{"workflow_id":"` + workflowID + `","stopped":"true"}`, wantErr: true},
		{name: "different workflow", response: `{"workflow_id":"approval-` + testID + `-v2","stopped":true}`, wantErr: true},
		{name: "not stopped", response: `{"workflow_id":"` + workflowID + `","stopped":false}`, wantErr: true},
		{name: "missing confirmation", response: `{"workflow_id":"` + workflowID + `"}`, wantErr: true},
		{name: "missing workflow ID", response: `{"stopped":true}`, wantErr: true},
		{name: "empty response", response: `{}`, wantErr: true},
		{name: "null response", response: `null`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bridge := &fakeBridge{result: json.RawMessage(tc.response), err: tc.bridgeErr}
			err := stopApprovalWorkflow(context.Background(), bridge, workflowID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if tc.bridgeErr != nil && !errors.Is(err, tc.bridgeErr) {
				t.Fatalf("expected original bridge error, got %v", err)
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "stop_approval" {
				t.Fatalf("unexpected bridge calls: %v", bridge.calls)
			}
			if len(bridge.payloads) != 1 {
				t.Fatalf("expected one payload, got %d", len(bridge.payloads))
			}
			payload, ok := bridge.payloads[0].(map[string]string)
			if !ok || len(payload) != 1 || payload["workflow_id"] != workflowID {
				t.Fatalf("unexpected stop payload: %v", bridge.payloads[0])
			}
		})
	}
}

func TestRequestApprovalRunDeletion(t *testing.T) {
	workflowID := "approval-" + testID + "-v1"
	runID := "specific-run"
	ids := `"workflow_id":"` + workflowID + `","run_id":"` + runID + `"`
	success := `{` + ids + `,"deletion_requested":true}`
	bridgeError := errors.New("bridge unavailable")
	for _, tc := range []struct {
		name      string
		response  string
		bridgeErr error
		wantErr   bool
	}{
		{name: "confirmed", response: success},
		{name: "bridge error", response: success, bridgeErr: bridgeError, wantErr: true},
		{name: "invalid JSON", response: `{`, wantErr: true},
		{name: "wrong confirmation type", response: `{` + ids + `,"deletion_requested":"true"}`, wantErr: true},
		{name: "different workflow", response: `{"workflow_id":"other-workflow","run_id":"` + runID + `","deletion_requested":true}`, wantErr: true},
		{name: "different run", response: `{"workflow_id":"` + workflowID + `","run_id":"other-run","deletion_requested":true}`, wantErr: true},
		{name: "not requested", response: `{` + ids + `,"deletion_requested":false}`, wantErr: true},
		{name: "missing confirmation", response: `{` + ids + `}`, wantErr: true},
		{name: "null confirmation", response: `{` + ids + `,"deletion_requested":null}`, wantErr: true},
		{name: "missing workflow ID", response: `{"run_id":"` + runID + `","deletion_requested":true}`, wantErr: true},
		{name: "missing run ID", response: `{"workflow_id":"` + workflowID + `","deletion_requested":true}`, wantErr: true},
		{name: "empty response", response: `{}`, wantErr: true},
		{name: "null response", response: `null`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bridge := &fakeBridge{result: json.RawMessage(tc.response), err: tc.bridgeErr}
			err := requestApprovalRunDeletion(context.Background(), bridge, workflowID, runID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if tc.bridgeErr != nil && !errors.Is(err, tc.bridgeErr) {
				t.Fatalf("expected original bridge error, got %v", err)
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "request_approval_run_deletion" {
				t.Fatalf("unexpected bridge calls: %v", bridge.calls)
			}
			if len(bridge.payloads) != 1 {
				t.Fatalf("expected one payload, got %d", len(bridge.payloads))
			}
			payload, ok := bridge.payloads[0].(map[string]string)
			if !ok || len(payload) != 2 || payload["workflow_id"] != workflowID || payload["run_id"] != runID {
				t.Fatalf("unexpected deletion request payload: %v", bridge.payloads[0])
			}
		})
	}
}

func TestApprovalRunDeleted(t *testing.T) {
	workflowID := "approval-" + testID + "-v1"
	runID := "specific-run"
	ids := `"workflow_id":"` + workflowID + `","run_id":"` + runID + `"`
	success := `{` + ids + `,"deleted":true}`
	bridgeError := errors.New("bridge unavailable")
	for _, tc := range []struct {
		name        string
		response    string
		bridgeErr   error
		wantDeleted bool
		wantErr     bool
	}{
		{name: "deleted", response: success, wantDeleted: true},
		{name: "still waiting", response: `{` + ids + `,"deleted":false}`},
		{name: "bridge error", response: success, bridgeErr: bridgeError, wantErr: true},
		{name: "invalid JSON", response: `{`, wantErr: true},
		{name: "wrong confirmation type", response: `{` + ids + `,"deleted":"true"}`, wantErr: true},
		{name: "different workflow", response: `{"workflow_id":"other-workflow","run_id":"` + runID + `","deleted":true}`, wantErr: true},
		{name: "different run", response: `{"workflow_id":"` + workflowID + `","run_id":"other-run","deleted":true}`, wantErr: true},
		{name: "missing confirmation", response: `{` + ids + `}`, wantErr: true},
		{name: "null confirmation", response: `{` + ids + `,"deleted":null}`, wantErr: true},
		{name: "missing workflow ID", response: `{"run_id":"` + runID + `","deleted":true}`, wantErr: true},
		{name: "missing run ID", response: `{"workflow_id":"` + workflowID + `","deleted":true}`, wantErr: true},
		{name: "empty response", response: `{}`, wantErr: true},
		{name: "null response", response: `null`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bridge := &fakeBridge{result: json.RawMessage(tc.response), err: tc.bridgeErr}
			deleted, err := approvalRunDeleted(context.Background(), bridge, workflowID, runID)
			if deleted != tc.wantDeleted || (err != nil) != tc.wantErr {
				t.Fatalf("expected deleted %v and error %v, got (%v, %v)", tc.wantDeleted, tc.wantErr, deleted, err)
			}
			if tc.bridgeErr != nil && !errors.Is(err, tc.bridgeErr) {
				t.Fatalf("expected original bridge error, got %v", err)
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "approval_run_deleted" {
				t.Fatalf("unexpected bridge calls: %v", bridge.calls)
			}
			if len(bridge.payloads) != 1 {
				t.Fatalf("expected one payload, got %d", len(bridge.payloads))
			}
			payload, ok := bridge.payloads[0].(map[string]string)
			if !ok || len(payload) != 2 || payload["workflow_id"] != workflowID || payload["run_id"] != runID {
				t.Fatalf("unexpected deletion status payload: %v", bridge.payloads[0])
			}
		})
	}
}

func TestApprovalRunDeletionRejectsInvalidIDs(t *testing.T) {
	workflowID := "approval-" + testID + "-v1"
	runID := "specific-run"
	for _, tc := range []struct {
		name       string
		workflowID string
		runID      string
	}{
		{name: "empty workflow ID", runID: runID},
		{name: "blank workflow ID", workflowID: " \t\n", runID: runID},
		{name: "empty run ID", workflowID: workflowID},
		{name: "blank run ID", workflowID: workflowID, runID: " \t\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("request", func(t *testing.T) {
				bridge := &fakeBridge{}
				if err := requestApprovalRunDeletion(context.Background(), bridge, tc.workflowID, tc.runID); err == nil {
					t.Fatal("expected invalid ID error")
				}
				if len(bridge.calls) != 0 {
					t.Fatalf("unexpected bridge calls: %v", bridge.calls)
				}
			})
			t.Run("status", func(t *testing.T) {
				bridge := &fakeBridge{}
				deleted, err := approvalRunDeleted(context.Background(), bridge, tc.workflowID, tc.runID)
				if deleted || err == nil {
					t.Fatalf("expected invalid ID error, got (%v, %v)", deleted, err)
				}
				if len(bridge.calls) != 0 {
					t.Fatalf("unexpected bridge calls: %v", bridge.calls)
				}
			})
		})
	}
}

type fakeAccountCleanupStore struct {
	sending   func(context.Context, string) (bool, error)
	workflows func(context.Context, string) ([]string, error)
}

func (s fakeAccountCleanupStore) HasSendingOutbox(ctx context.Context, accountID string) (bool, error) {
	return s.sending(ctx, accountID)
}

func (s fakeAccountCleanupStore) DeletionWorkflowIDs(ctx context.Context, accountID string) ([]string, error) {
	return s.workflows(ctx, accountID)
}

type cleanupBridgeFunc func(context.Context, string, any) (json.RawMessage, error)

func (f cleanupBridgeFunc) Call(ctx context.Context, action string, payload any) (json.RawMessage, error) {
	return f(ctx, action, payload)
}

func TestAccountCleanerStopWorkflows(t *testing.T) {
	lookupError := errors.New("lookup unavailable")
	stopError := errors.New("stop unavailable")
	workflowIDs := []string{
		"approval-" + testID + "-v1",
		"approval-" + testID + "-v2",
		"approval-" + testID + "-v3",
	}
	for _, tc := range []struct {
		name        string
		sending     bool
		sendingErr  error
		workflowIDs []string
		workflowErr error
		failedID    string
		wantStopped bool
		wantErr     error
		wantCalls   []string
	}{
		{
			name:      "wait for delivery",
			sending:   true,
			wantCalls: []string{"sending"},
		},
		{
			name:       "delivery lookup error",
			sending:    true,
			sendingErr: lookupError,
			wantErr:    lookupError,
			wantCalls:  []string{"sending"},
		},
		{
			name:        "workflow lookup error",
			workflowIDs: workflowIDs,
			workflowErr: lookupError,
			wantErr:     lookupError,
			wantCalls:   []string{"sending", "workflows"},
		},
		{
			name:        "no workflows",
			wantStopped: true,
			wantCalls:   []string{"sending", "workflows"},
		},
		{
			name:        "stop all workflows",
			workflowIDs: workflowIDs,
			wantStopped: true,
			wantCalls:   []string{"sending", "workflows", workflowIDs[0], workflowIDs[1], workflowIDs[2]},
		},
		{
			name:        "stop at first failure",
			workflowIDs: workflowIDs,
			failedID:    workflowIDs[1],
			wantErr:     stopError,
			wantCalls:   []string{"sending", "workflows", workflowIDs[0], workflowIDs[1]},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			store := fakeAccountCleanupStore{
				sending: func(gotCtx context.Context, accountID string) (bool, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("delivery lookup received the wrong context or account")
					}
					calls = append(calls, "sending")
					return tc.sending, tc.sendingErr
				},
				workflows: func(gotCtx context.Context, accountID string) ([]string, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("workflow lookup received the wrong context or account")
					}
					calls = append(calls, "workflows")
					return tc.workflowIDs, tc.workflowErr
				},
			}
			bridge := cleanupBridgeFunc(func(gotCtx context.Context, action string, payload any) (json.RawMessage, error) {
				if gotCtx != ctx || action != "stop_approval" {
					t.Fatal("bridge received the wrong context or action")
				}
				input, ok := payload.(map[string]string)
				if !ok || len(input) != 1 || input["workflow_id"] == "" {
					t.Fatalf("unexpected stop payload: %v", payload)
				}
				workflowID := input["workflow_id"]
				calls = append(calls, workflowID)
				if workflowID == tc.failedID {
					return nil, stopError
				}
				return json.RawMessage(`{"workflow_id":"` + workflowID + `","stopped":true}`), nil
			})
			cleaner := AccountCleaner{Accounts: store, Bridge: bridge}
			stopped, err := cleaner.stopWorkflows(ctx, testID)
			if stopped != tc.wantStopped || !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected (%v, %v), got (%v, %v)", tc.wantStopped, tc.wantErr, stopped, err)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Fatalf("expected calls %v, got %v", tc.wantCalls, calls)
			}
		})
	}
}

func TestAccountCleanerPrepareDeletion(t *testing.T) {
	t.Run("missing Gmail cleanup", func(t *testing.T) {
		cleaner := AccountCleaner{}
		ready, err := cleaner.prepareDeletion(context.Background(), testID)
		if ready || err == nil || err.Error() != "gmail cleanup is not configured" {
			t.Fatalf("expected Gmail configuration error, got (%v, %v)", ready, err)
		}
	})

	lookupError := errors.New("lookup unavailable")
	stopError := errors.New("stop unavailable")
	removalError := errors.New("token removal failed")
	workflowIDs := []string{
		"approval-" + testID + "-v1",
		"approval-" + testID + "-v2",
	}
	for _, tc := range []struct {
		name        string
		sending     bool
		sendingErr  error
		noWorkflows bool
		stopErr     error
		removalErr  error
		wantReady   bool
		wantErr     error
		wantCalls   []string
	}{
		{
			name:      "wait for delivery",
			sending:   true,
			wantCalls: []string{"sending"},
		},
		{
			name:       "delivery lookup error",
			sendingErr: lookupError,
			wantErr:    lookupError,
			wantCalls:  []string{"sending"},
		},
		{
			name:      "workflow stop error",
			stopErr:   stopError,
			wantErr:   stopError,
			wantCalls: []string{"sending", "workflows", workflowIDs[0]},
		},
		{
			name:       "token removal error",
			removalErr: removalError,
			wantErr:    removalError,
			wantCalls:  []string{"sending", "workflows", workflowIDs[0], workflowIDs[1], "remove token"},
		},
		{
			name:        "no workflows",
			noWorkflows: true,
			wantReady:   true,
			wantCalls:   []string{"sending", "workflows", "remove token"},
		},
		{
			name:      "remove token after all workflows stop",
			wantReady: true,
			wantCalls: []string{"sending", "workflows", workflowIDs[0], workflowIDs[1], "remove token"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			store := fakeAccountCleanupStore{
				sending: func(gotCtx context.Context, accountID string) (bool, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("delivery lookup received the wrong context or account")
					}
					calls = append(calls, "sending")
					return tc.sending, tc.sendingErr
				},
				workflows: func(gotCtx context.Context, accountID string) ([]string, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("workflow lookup received the wrong context or account")
					}
					calls = append(calls, "workflows")
					if tc.noWorkflows {
						return nil, nil
					}
					return workflowIDs, nil
				},
			}
			bridge := cleanupBridgeFunc(func(gotCtx context.Context, action string, payload any) (json.RawMessage, error) {
				if gotCtx != ctx || action != "stop_approval" {
					t.Fatal("bridge received the wrong context or action")
				}
				input, ok := payload.(map[string]string)
				if !ok || len(input) != 1 || input["workflow_id"] == "" {
					t.Fatalf("unexpected stop payload: %v", payload)
				}
				workflowID := input["workflow_id"]
				calls = append(calls, workflowID)
				if tc.stopErr != nil {
					return nil, tc.stopErr
				}
				return json.RawMessage(`{"workflow_id":"` + workflowID + `","stopped":true}`), nil
			})
			cleaner := AccountCleaner{
				Accounts: store,
				Bridge:   bridge,
				RemoveGmailToken: func(accountID string) error {
					if accountID != testID {
						t.Fatalf("token removal received the wrong account: %q", accountID)
					}
					calls = append(calls, "remove token")
					return tc.removalErr
				},
			}
			ready, err := cleaner.prepareDeletion(ctx, testID)
			if ready != tc.wantReady || !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected (%v, %v), got (%v, %v)", tc.wantReady, tc.wantErr, ready, err)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Fatalf("expected calls %v, got %v", tc.wantCalls, calls)
			}
		})
	}
}
