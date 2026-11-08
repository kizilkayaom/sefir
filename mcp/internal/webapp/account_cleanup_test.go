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

func TestListApprovalRuns(t *testing.T) {
	workflowID := "approval-" + testID + "-v1"
	id := `"workflow_id":"` + workflowID + `"`
	success := `{` + id + `,"run_ids":["run-one","run-two"]}`
	bridgeError := errors.New("bridge unavailable")
	for _, tc := range []struct {
		name      string
		response  string
		bridgeErr error
		wantRuns  []string
		wantErr   bool
	}{
		{name: "runs found", response: success, wantRuns: []string{"run-one", "run-two"}},
		{name: "no runs", response: `{` + id + `,"run_ids":[]}`, wantRuns: []string{}},
		{name: "bridge error", response: success, bridgeErr: bridgeError, wantErr: true},
		{name: "invalid JSON", response: `{`, wantErr: true},
		{name: "different workflow", response: `{"workflow_id":"other-workflow","run_ids":[]}`, wantErr: true},
		{name: "missing workflow ID", response: `{"run_ids":[]}`, wantErr: true},
		{name: "null workflow ID", response: `{"workflow_id":null,"run_ids":[]}`, wantErr: true},
		{name: "wrong workflow ID type", response: `{"workflow_id":123,"run_ids":[]}`, wantErr: true},
		{name: "missing runs", response: `{` + id + `}`, wantErr: true},
		{name: "null runs", response: `{` + id + `,"run_ids":null}`, wantErr: true},
		{name: "wrong runs type", response: `{` + id + `,"run_ids":"run-one"}`, wantErr: true},
		{name: "wrong run ID type", response: `{` + id + `,"run_ids":[123]}`, wantErr: true},
		{name: "null run ID", response: `{` + id + `,"run_ids":[null]}`, wantErr: true},
		{name: "empty run ID", response: `{` + id + `,"run_ids":[""]}`, wantErr: true},
		{name: "blank run ID", response: `{` + id + `,"run_ids":[" \t\n"]}`, wantErr: true},
		{name: "invalid run after valid run", response: `{` + id + `,"run_ids":["run-one",""]}`, wantErr: true},
		{name: "empty response", response: `{}`, wantErr: true},
		{name: "null response", response: `null`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bridge := &fakeBridge{result: json.RawMessage(tc.response), err: tc.bridgeErr}
			runs, err := listApprovalRuns(context.Background(), bridge, workflowID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if !slices.Equal(runs, tc.wantRuns) || (runs == nil) != (tc.wantRuns == nil) {
				t.Fatalf("expected runs %#v, got %#v", tc.wantRuns, runs)
			}
			if tc.bridgeErr != nil && !errors.Is(err, tc.bridgeErr) {
				t.Fatalf("expected original bridge error, got %v", err)
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "list_approval_runs" {
				t.Fatalf("unexpected bridge calls: %v", bridge.calls)
			}
			if len(bridge.payloads) != 1 {
				t.Fatalf("expected one payload, got %d", len(bridge.payloads))
			}
			payload, ok := bridge.payloads[0].(map[string]string)
			if !ok || len(payload) != 1 || payload["workflow_id"] != workflowID {
				t.Fatalf("unexpected run listing payload: %v", bridge.payloads[0])
			}
		})
	}
}

func TestListApprovalRunsRejectsInvalidWorkflowIDs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		workflowID string
	}{
		{name: "empty workflow ID"},
		{name: "blank workflow ID", workflowID: " \t\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bridge := &fakeBridge{}
			runs, err := listApprovalRuns(context.Background(), bridge, tc.workflowID)
			if err == nil || runs != nil {
				t.Fatalf("expected invalid ID error and no runs, got (%v, %v)", runs, err)
			}
			if len(bridge.calls) != 0 {
				t.Fatalf("unexpected bridge calls: %v", bridge.calls)
			}
		})
	}
}

type fakeAccountCleanupStore struct {
	sending   func(context.Context, string) (bool, error)
	workflows func(context.Context, string) ([]string, error)
	saveRun   func(context.Context, string, string, string) error
	runs      func(context.Context, string) ([]DeletionRun, error)
}

func (s fakeAccountCleanupStore) HasSendingOutbox(ctx context.Context, accountID string) (bool, error) {
	return s.sending(ctx, accountID)
}

func (s fakeAccountCleanupStore) DeletionWorkflowIDs(ctx context.Context, accountID string) ([]string, error) {
	return s.workflows(ctx, accountID)
}

func (s fakeAccountCleanupStore) SaveDeletionRun(ctx context.Context, accountID, workflowID, runID string) error {
	return s.saveRun(ctx, accountID, workflowID, runID)
}

func (s fakeAccountCleanupStore) DeletionRuns(ctx context.Context, accountID string) ([]DeletionRun, error) {
	return s.runs(ctx, accountID)
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

func TestAccountCleanerRecordDeletionRuns(t *testing.T) {
	lookupError := errors.New("lookup unavailable")
	listingError := errors.New("listing unavailable")
	saveError := errors.New("save failed")
	workflowIDs := []string{
		"approval-" + testID + "-v1",
		"approval-" + testID + "-v2",
		"approval-" + testID + "-v3",
	}
	runs := []DeletionRun{
		{WorkflowID: workflowIDs[0], RunID: "run-one"},
		{WorkflowID: workflowIDs[0], RunID: "run-two"},
		{WorkflowID: workflowIDs[2], RunID: "run-one"},
	}
	discovered := map[string][]string{
		workflowIDs[0]: {"run-one", "run-two"},
		workflowIDs[2]: {"run-one"},
	}
	for _, tc := range []struct {
		name           string
		workflowIDs    []string
		workflowErr    error
		runIDs         map[string][]string
		listingFailure string
		saveFailure    DeletionRun
		wantErr        error
		wantCalls      []string
		wantSaved      []DeletionRun
	}{
		{
			name:      "no workflows",
			wantCalls: []string{"workflows"},
		},
		{
			name:        "no discovered runs",
			workflowIDs: workflowIDs,
			wantCalls:   []string{"workflows", "list:" + workflowIDs[0], "list:" + workflowIDs[1], "list:" + workflowIDs[2]},
		},
		{
			name:        "save all runs and continue past empty workflows",
			workflowIDs: workflowIDs,
			runIDs:      discovered,
			wantCalls: []string{
				"workflows",
				"list:" + workflowIDs[0],
				"save:" + workflowIDs[0] + ":run-one",
				"save:" + workflowIDs[0] + ":run-two",
				"list:" + workflowIDs[1],
				"list:" + workflowIDs[2],
				"save:" + workflowIDs[2] + ":run-one",
			},
			wantSaved: runs,
		},
		{
			name:        "workflow lookup error",
			workflowIDs: workflowIDs,
			workflowErr: lookupError,
			wantErr:     lookupError,
			wantCalls:   []string{"workflows"},
		},
		{
			name:           "listing error retains earlier saves",
			workflowIDs:    workflowIDs,
			runIDs:         discovered,
			listingFailure: workflowIDs[1],
			wantErr:        listingError,
			wantCalls: []string{
				"workflows",
				"list:" + workflowIDs[0],
				"save:" + workflowIDs[0] + ":run-one",
				"save:" + workflowIDs[0] + ":run-two",
				"list:" + workflowIDs[1],
			},
			wantSaved: runs[:2],
		},
		{
			name:        "save error retains earlier saves",
			workflowIDs: workflowIDs,
			runIDs:      discovered,
			saveFailure: runs[1],
			wantErr:     saveError,
			wantCalls: []string{
				"workflows",
				"list:" + workflowIDs[0],
				"save:" + workflowIDs[0] + ":run-one",
				"save:" + workflowIDs[0] + ":run-two",
			},
			wantSaved: runs[:1],
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			var saved []DeletionRun
			store := fakeAccountCleanupStore{
				workflows: func(gotCtx context.Context, accountID string) ([]string, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("workflow lookup received the wrong context or account")
					}
					calls = append(calls, "workflows")
					return tc.workflowIDs, tc.workflowErr
				},
				saveRun: func(gotCtx context.Context, accountID, workflowID, runID string) error {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("run save received the wrong context or account")
					}
					calls = append(calls, "save:"+workflowID+":"+runID)
					run := DeletionRun{WorkflowID: workflowID, RunID: runID}
					if run == tc.saveFailure {
						return saveError
					}
					saved = append(saved, run)
					return nil
				},
			}
			bridge := cleanupBridgeFunc(func(gotCtx context.Context, action string, payload any) (json.RawMessage, error) {
				if gotCtx != ctx || action != "list_approval_runs" {
					t.Fatal("bridge received the wrong context or action")
				}
				input, ok := payload.(map[string]string)
				if !ok || len(input) != 1 || !slices.Contains(tc.workflowIDs, input["workflow_id"]) {
					t.Fatalf("unexpected run listing payload: %v", payload)
				}
				workflowID := input["workflow_id"]
				calls = append(calls, "list:"+workflowID)
				if workflowID == tc.listingFailure {
					return nil, listingError
				}
				runIDs := tc.runIDs[workflowID]
				if runIDs == nil {
					runIDs = []string{}
				}
				return json.Marshal(struct {
					WorkflowID string   `json:"workflow_id"`
					RunIDs     []string `json:"run_ids"`
				}{WorkflowID: workflowID, RunIDs: runIDs})
			})
			cleaner := AccountCleaner{Accounts: store, Bridge: bridge}
			err := cleaner.recordDeletionRuns(ctx, testID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Fatalf("expected calls %v, got %v", tc.wantCalls, calls)
			}
			if !slices.Equal(saved, tc.wantSaved) {
				t.Fatalf("expected saved runs %v, got %v", tc.wantSaved, saved)
			}
		})
	}
}

func TestAccountCleanerDeleteRecordedRuns(t *testing.T) {
	lookupError := errors.New("lookup unavailable")
	statusError := errors.New("status unavailable")
	requestError := errors.New("deletion request failed")
	runs := []DeletionRun{
		{WorkflowID: "approval-" + testID + "-v1", RunID: "run-one"},
		{WorkflowID: "approval-" + testID + "-v1", RunID: "run-two"},
		{WorkflowID: "approval-" + testID + "-v2", RunID: "run-one"},
	}
	statusCalls := []string{
		"status:" + runs[0].WorkflowID + ":" + runs[0].RunID,
		"status:" + runs[1].WorkflowID + ":" + runs[1].RunID,
		"status:" + runs[2].WorkflowID + ":" + runs[2].RunID,
	}
	requestCalls := []string{
		"request:" + runs[0].WorkflowID + ":" + runs[0].RunID,
		"request:" + runs[1].WorkflowID + ":" + runs[1].RunID,
		"request:" + runs[2].WorkflowID + ":" + runs[2].RunID,
	}
	for _, tc := range []struct {
		name           string
		runs           []DeletionRun
		lookupErr      error
		deleted        []DeletionRun
		statusFailure  DeletionRun
		requestFailure DeletionRun
		wantDeleted    bool
		wantErr        error
		wantCalls      []string
	}{
		{
			name:        "no saved runs",
			wantDeleted: true,
			wantCalls:   []string{"runs"},
		},
		{
			name:      "lookup error",
			runs:      runs,
			lookupErr: lookupError,
			wantErr:   lookupError,
			wantCalls: []string{"runs"},
		},
		{
			name:        "all runs already deleted",
			runs:        runs,
			deleted:     runs,
			wantDeleted: true,
			wantCalls:   []string{"runs", statusCalls[0], statusCalls[1], statusCalls[2]},
		},
		{
			name:      "request every pending run",
			runs:      runs,
			wantCalls: []string{"runs", statusCalls[0], requestCalls[0], statusCalls[1], requestCalls[1], statusCalls[2], requestCalls[2]},
		},
		{
			name:      "skip deleted runs while still waiting",
			runs:      runs,
			deleted:   runs[1:],
			wantCalls: []string{"runs", statusCalls[0], requestCalls[0], statusCalls[1], statusCalls[2]},
		},
		{
			name:          "stop at status failure",
			runs:          runs,
			deleted:       runs[:1],
			statusFailure: runs[1],
			wantErr:       statusError,
			wantCalls:     []string{"runs", statusCalls[0], statusCalls[1]},
		},
		{
			name:           "stop at request failure",
			runs:           runs,
			requestFailure: runs[1],
			wantErr:        requestError,
			wantCalls:      []string{"runs", statusCalls[0], requestCalls[0], statusCalls[1], requestCalls[1]},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			store := fakeAccountCleanupStore{
				runs: func(gotCtx context.Context, accountID string) ([]DeletionRun, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("run lookup received the wrong context or account")
					}
					calls = append(calls, "runs")
					return tc.runs, tc.lookupErr
				},
			}
			bridge := cleanupBridgeFunc(func(gotCtx context.Context, action string, payload any) (json.RawMessage, error) {
				if gotCtx != ctx {
					t.Fatal("bridge received the wrong context")
				}
				input, ok := payload.(map[string]string)
				if !ok || len(input) != 2 {
					t.Fatalf("unexpected run deletion payload: %v", payload)
				}
				run := DeletionRun{WorkflowID: input["workflow_id"], RunID: input["run_id"]}
				if !slices.Contains(tc.runs, run) {
					t.Fatalf("unexpected workflow run: %v", run)
				}
				response := map[string]any{"workflow_id": run.WorkflowID, "run_id": run.RunID}
				switch action {
				case "approval_run_deleted":
					calls = append(calls, "status:"+run.WorkflowID+":"+run.RunID)
					if run == tc.statusFailure {
						return nil, statusError
					}
					response["deleted"] = slices.Contains(tc.deleted, run)
				case "request_approval_run_deletion":
					calls = append(calls, "request:"+run.WorkflowID+":"+run.RunID)
					if run == tc.requestFailure {
						return nil, requestError
					}
					response["deletion_requested"] = true
				default:
					t.Fatalf("unexpected bridge action: %q", action)
				}
				return json.Marshal(response)
			})
			cleaner := AccountCleaner{Accounts: store, Bridge: bridge}
			deleted, err := cleaner.deleteRecordedRuns(ctx, testID)
			if deleted != tc.wantDeleted || !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected (%v, %v), got (%v, %v)", tc.wantDeleted, tc.wantErr, deleted, err)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Fatalf("expected calls %v, got %v", tc.wantCalls, calls)
			}
		})
	}
}

func TestAccountCleanerDeleteRecordedRunsRetriesUntilDeleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := []DeletionRun{
		{WorkflowID: "approval-" + testID + "-v1", RunID: "run-one"},
		{WorkflowID: "approval-" + testID + "-v1", RunID: "run-two"},
		{WorkflowID: "approval-" + testID + "-v2", RunID: "run-one"},
	}
	stored := slices.Clone(runs)
	var deletedRuns, checked, requested []DeletionRun
	var lookups int
	store := fakeAccountCleanupStore{
		runs: func(gotCtx context.Context, accountID string) ([]DeletionRun, error) {
			if gotCtx != ctx || accountID != testID {
				t.Fatal("run lookup received the wrong context or account")
			}
			lookups++
			return stored, nil
		},
	}
	bridge := cleanupBridgeFunc(func(gotCtx context.Context, action string, payload any) (json.RawMessage, error) {
		if gotCtx != ctx {
			t.Fatal("bridge received the wrong context")
		}
		input, ok := payload.(map[string]string)
		if !ok || len(input) != 2 {
			t.Fatalf("unexpected run deletion payload: %v", payload)
		}
		run := DeletionRun{WorkflowID: input["workflow_id"], RunID: input["run_id"]}
		if !slices.Contains(runs, run) {
			t.Fatalf("unexpected workflow run: %v", run)
		}
		response := map[string]any{"workflow_id": run.WorkflowID, "run_id": run.RunID}
		switch action {
		case "approval_run_deleted":
			checked = append(checked, run)
			response["deleted"] = slices.Contains(deletedRuns, run)
		case "request_approval_run_deletion":
			requested = append(requested, run)
			response["deletion_requested"] = true
		default:
			t.Fatalf("unexpected bridge action: %q", action)
		}
		return json.Marshal(response)
	})
	cleaner := AccountCleaner{Accounts: store, Bridge: bridge}
	for _, tc := range []struct {
		name          string
		deleted       []DeletionRun
		wantDeleted   bool
		wantRequested []DeletionRun
	}{
		{name: "initial requests", wantRequested: runs},
		{name: "partial completion", deleted: runs[:1], wantRequested: runs[1:]},
		{name: "all deleted", deleted: runs, wantDeleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deletedRuns = tc.deleted
			checked, requested = nil, nil
			lookups = 0
			deleted, err := cleaner.deleteRecordedRuns(ctx, testID)
			if err != nil || deleted != tc.wantDeleted {
				t.Fatalf("expected deleted %v, got (%v, %v)", tc.wantDeleted, deleted, err)
			}
			if lookups != 1 || !slices.Equal(checked, runs) {
				t.Fatalf("expected one lookup and checks for %v, got %d lookups and %v", runs, lookups, checked)
			}
			if !slices.Equal(requested, tc.wantRequested) {
				t.Fatalf("expected deletion requests %v, got %v", tc.wantRequested, requested)
			}
			if !slices.Equal(stored, runs) {
				t.Fatalf("saved tracking rows changed: %v", stored)
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

	stageError := errors.New("cleanup stage failed")
	workflowIDs := []string{
		"approval-" + testID + "-v1",
		"approval-" + testID + "-v2",
	}
	runs := []DeletionRun{
		{WorkflowID: workflowIDs[0], RunID: "run-one"},
		{WorkflowID: workflowIDs[1], RunID: "run-two"},
	}
	stopCalls := []string{"stop:" + workflowIDs[0], "stop:" + workflowIDs[1]}
	listCalls := []string{"list:" + workflowIDs[0], "list:" + workflowIDs[1]}
	saveCalls := []string{
		"save:" + runs[0].WorkflowID + ":" + runs[0].RunID,
		"save:" + runs[1].WorkflowID + ":" + runs[1].RunID,
	}
	statusCalls := []string{
		"status:" + runs[0].WorkflowID + ":" + runs[0].RunID,
		"status:" + runs[1].WorkflowID + ":" + runs[1].RunID,
	}
	requestCalls := []string{
		"request:" + runs[0].WorkflowID + ":" + runs[0].RunID,
		"request:" + runs[1].WorkflowID + ":" + runs[1].RunID,
	}
	preparationCalls := []string{
		"sending", "workflows", stopCalls[0], stopCalls[1], "remove token",
		"discover workflows", listCalls[0], saveCalls[0], listCalls[1], saveCalls[1], "runs",
	}
	pendingCalls := append(slices.Clone(preparationCalls), statusCalls[0], requestCalls[0], statusCalls[1], requestCalls[1])
	for _, tc := range []struct {
		name        string
		sending     bool
		noWorkflows bool
		noRuns      bool
		allDeleted  bool
		failedCall  string
		wantReady   bool
		wantCalls   []string
		wantSaved   []DeletionRun
	}{
		{
			name:      "wait for delivery",
			sending:   true,
			wantCalls: []string{"sending"},
		},
		{
			name:       "delivery lookup error",
			failedCall: "sending",
		},
		{
			name:       "workflow lookup error",
			failedCall: "workflows",
		},
		{
			name:       "workflow stop error",
			failedCall: stopCalls[1],
		},
		{
			name:       "token removal error",
			failedCall: "remove token",
		},
		{
			name:       "discovery lookup error",
			failedCall: "discover workflows",
		},
		{
			name:       "listing error prevents deletion checks",
			failedCall: listCalls[1],
			wantSaved:  runs[:1],
		},
		{
			name:       "save error prevents deletion checks",
			failedCall: saveCalls[1],
			wantSaved:  runs[:1],
		},
		{
			name:       "saved run lookup error",
			failedCall: "runs",
			wantSaved:  runs,
		},
		{
			name:       "deletion status error",
			failedCall: statusCalls[1],
			wantSaved:  runs,
		},
		{
			name:       "deletion request error",
			failedCall: requestCalls[0],
			wantSaved:  runs,
		},
		{
			name:        "no workflows",
			noWorkflows: true,
			wantReady:   true,
			wantCalls:   []string{"sending", "workflows", "remove token", "discover workflows", "runs"},
		},
		{
			name:      "no discovered runs",
			noRuns:    true,
			wantReady: true,
			wantCalls: []string{
				"sending", "workflows", stopCalls[0], stopCalls[1], "remove token",
				"discover workflows", listCalls[0], listCalls[1], "runs",
			},
		},
		{
			name:      "save all runs before requesting deletion",
			wantCalls: pendingCalls,
			wantSaved: runs,
		},
		{
			name:       "all recorded runs confirmed deleted",
			allDeleted: true,
			wantReady:  true,
			wantCalls:  append(slices.Clone(preparationCalls), statusCalls[0], statusCalls[1]),
			wantSaved:  runs,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			var saved []DeletionRun
			var workflowLookups int
			recordCall := func(name string) error {
				calls = append(calls, name)
				if name == tc.failedCall {
					return stageError
				}
				return nil
			}
			store := fakeAccountCleanupStore{
				sending: func(gotCtx context.Context, accountID string) (bool, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("delivery lookup received the wrong context or account")
					}
					return tc.sending, recordCall("sending")
				},
				workflows: func(gotCtx context.Context, accountID string) ([]string, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("workflow lookup received the wrong context or account")
					}
					workflowLookups++
					name := "workflows"
					if workflowLookups > 1 {
						name = "discover workflows"
					}
					if err := recordCall(name); err != nil {
						return nil, err
					}
					if tc.noWorkflows {
						return nil, nil
					}
					return workflowIDs, nil
				},
				saveRun: func(gotCtx context.Context, accountID, workflowID, runID string) error {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("run save received the wrong context or account")
					}
					run := DeletionRun{WorkflowID: workflowID, RunID: runID}
					if !slices.Contains(runs, run) {
						t.Fatalf("unexpected saved run: %v", run)
					}
					if err := recordCall("save:" + workflowID + ":" + runID); err != nil {
						return err
					}
					saved = append(saved, run)
					return nil
				},
				runs: func(gotCtx context.Context, accountID string) ([]DeletionRun, error) {
					if gotCtx != ctx || accountID != testID {
						t.Fatal("run lookup received the wrong context or account")
					}
					if err := recordCall("runs"); err != nil {
						return nil, err
					}
					return slices.Clone(saved), nil
				},
			}
			bridge := cleanupBridgeFunc(func(gotCtx context.Context, action string, payload any) (json.RawMessage, error) {
				if gotCtx != ctx {
					t.Fatal("bridge received the wrong context")
				}
				input, ok := payload.(map[string]string)
				if !ok || !slices.Contains(workflowIDs, input["workflow_id"]) {
					t.Fatalf("unexpected cleanup payload: %v", payload)
				}
				workflowID := input["workflow_id"]
				response := map[string]any{"workflow_id": workflowID}
				switch action {
				case "stop_approval", "list_approval_runs":
					if len(input) != 1 {
						t.Fatalf("unexpected workflow payload: %v", payload)
					}
					if action == "stop_approval" {
						if err := recordCall("stop:" + workflowID); err != nil {
							return nil, err
						}
						response["stopped"] = true
					} else {
						if err := recordCall("list:" + workflowID); err != nil {
							return nil, err
						}
						runIDs := []string{}
						if !tc.noRuns {
							runIDs = append(runIDs, runs[slices.Index(workflowIDs, workflowID)].RunID)
						}
						response["run_ids"] = runIDs
					}
				case "approval_run_deleted", "request_approval_run_deletion":
					run := DeletionRun{WorkflowID: workflowID, RunID: input["run_id"]}
					if len(input) != 2 || !slices.Contains(saved, run) {
						t.Fatalf("unexpected deletion payload: %v", payload)
					}
					response["run_id"] = run.RunID
					if action == "approval_run_deleted" {
						if err := recordCall("status:" + workflowID + ":" + run.RunID); err != nil {
							return nil, err
						}
						response["deleted"] = tc.allDeleted
					} else {
						if err := recordCall("request:" + workflowID + ":" + run.RunID); err != nil {
							return nil, err
						}
						response["deletion_requested"] = true
					}
				default:
					t.Fatalf("unexpected bridge action: %q", action)
				}
				return json.Marshal(response)
			})
			cleaner := AccountCleaner{
				Accounts: store,
				Bridge:   bridge,
				RemoveGmailToken: func(accountID string) error {
					if accountID != testID {
						t.Fatalf("token removal received the wrong account: %q", accountID)
					}
					return recordCall("remove token")
				},
			}
			var wantErr error
			wantCalls := tc.wantCalls
			if tc.failedCall != "" {
				wantErr = stageError
				index := slices.Index(pendingCalls, tc.failedCall)
				if index < 0 {
					t.Fatalf("unknown failure stage: %q", tc.failedCall)
				}
				wantCalls = pendingCalls[:index+1]
			}
			ready, err := cleaner.prepareDeletion(ctx, testID)
			if ready != tc.wantReady || !errors.Is(err, wantErr) {
				t.Fatalf("expected (%v, %v), got (%v, %v)", tc.wantReady, wantErr, ready, err)
			}
			if !slices.Equal(calls, wantCalls) {
				t.Fatalf("expected calls %v, got %v", wantCalls, calls)
			}
			if !slices.Equal(saved, tc.wantSaved) {
				t.Fatalf("expected saved runs %v, got %v", tc.wantSaved, saved)
			}
		})
	}
}
