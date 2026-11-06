package webapp

import (
	"context"
	"encoding/json"
	"errors"
)

type AccountCleanupStore interface {
	HasSendingOutbox(context.Context, string) (bool, error)
	DeletionWorkflowIDs(context.Context, string) ([]string, error)
}

type AccountCleaner struct {
	Accounts AccountCleanupStore
	Bridge   Bridge
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
