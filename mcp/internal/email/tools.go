package email

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func RegisterTools(server *mcp.Server, pool *pgxpool.Pool) {
	approvalOutputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"approval_id": {"type": "string", "format": "uuid"},
			"draft_id": {"type": "string", "format": "uuid"},
			"version": {"type": "integer", "minimum": 1},
			"content_hash": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
			"approved_by": {"type": "string", "minLength": 1},
			"approved_at": {"type": "string", "format": "date-time"},
			"outbox_id": {"type": "string", "format": "uuid"},
			"outbox_state": {
				"type": "string",
				"enum": ["pending", "sending", "sent", "failed", "needs_attention"]
			}
		},
		"required": [
			"approval_id", "draft_id", "version", "content_hash",
			"approved_by", "approved_at", "outbox_id", "outbox_state"
		],
		"additionalProperties": false
	}`)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "approve_draft",
		Description:  "Record human approval of an exact draft version and queue it for sending.",
		OutputSchema: approvalOutputSchema,
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input ApproveDraftInput,
	) (*mcp.CallToolResult, ApprovalResult, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		approval, err := ApproveDraft(ctx, pool, input)
		if err != nil {
			return nil, ApprovalResult{}, err
		}
		return nil, approval, nil
	})
}
