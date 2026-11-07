package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type AccountCleanupStore interface {
	HasSendingOutbox(context.Context, string) (bool, error)
	DeletionWorkflowIDs(context.Context, string) ([]string, error)
}

type AccountCleaner struct {
	Accounts         AccountCleanupStore
	Bridge           Bridge
	RemoveGmailToken func(string) error
}

func (c AccountCleaner) prepareDeletion(ctx context.Context, accountID string) (bool, error) {
	if c.RemoveGmailToken == nil {
		return false, errors.New("gmail cleanup is not configured")
	}

	stopped, err := c.stopWorkflows(ctx, accountID)
	if err != nil || !stopped {
		return false, err
	}
	if err := c.RemoveGmailToken(accountID); err != nil {
		return false, err
	}
	return true, nil
}

func (c AccountCleaner) stopWorkflows(ctx context.Context, accountID string) (bool, error) {
	sending, err := c.Accounts.HasSendingOutbox(ctx, accountID)
	if err != nil {
		return false, err
	}
	if sending {
		return false, nil
	}

	workflowIDs, err := c.Accounts.DeletionWorkflowIDs(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, workflowID := range workflowIDs {
		if err := stopApprovalWorkflow(ctx, c.Bridge, workflowID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func stopApprovalWorkflow(ctx context.Context, bridge Bridge, workflowID string) error {
	response, err := bridge.Call(ctx, "stop_approval", map[string]string{"workflow_id": workflowID})
	if err != nil {
		return err
	}

	var result struct {
		WorkflowID string `json:"workflow_id"`
		Stopped    bool   `json:"stopped"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return err
	}
	if result.WorkflowID != workflowID || !result.Stopped {
		return errors.New("agent did not confirm workflow stopped")
	}
	return nil
}

func requestApprovalRunDeletion(ctx context.Context, bridge Bridge, workflowID, runID string) error {
	if strings.TrimSpace(workflowID) == "" {
		return errors.New("workflow ID is required")
	}
	if strings.TrimSpace(runID) == "" {
		return errors.New("run ID is required")
	}

	response, err := bridge.Call(ctx, "request_approval_run_deletion", map[string]string{
		"workflow_id": workflowID,
		"run_id":      runID,
	})
	if err != nil {
		return err
	}

	var result struct {
		WorkflowID        string `json:"workflow_id"`
		RunID             string `json:"run_id"`
		DeletionRequested bool   `json:"deletion_requested"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return err
	}
	if result.WorkflowID != workflowID || result.RunID != runID || !result.DeletionRequested {
		return errors.New("agent did not confirm workflow run deletion request")
	}
	return nil
}

func approvalRunDeleted(ctx context.Context, bridge Bridge, workflowID, runID string) (bool, error) {
	if strings.TrimSpace(workflowID) == "" {
		return false, errors.New("workflow ID is required")
	}
	if strings.TrimSpace(runID) == "" {
		return false, errors.New("run ID is required")
	}

	response, err := bridge.Call(ctx, "approval_run_deleted", map[string]string{
		"workflow_id": workflowID,
		"run_id":      runID,
	})
	if err != nil {
		return false, err
	}

	var result struct {
		WorkflowID string `json:"workflow_id"`
		RunID      string `json:"run_id"`
		Deleted    *bool  `json:"deleted"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return false, err
	}
	if result.WorkflowID != workflowID || result.RunID != runID || result.Deleted == nil {
		return false, errors.New("agent did not confirm workflow run deletion status")
	}
	return *result.Deleted, nil
}
