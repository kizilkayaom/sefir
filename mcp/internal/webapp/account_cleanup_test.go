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
