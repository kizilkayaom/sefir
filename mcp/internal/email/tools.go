package email

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SendOutboxInput struct {
	OutboxID string `json:"outbox_id"`
}

func RegisterTools(server *mcp.Server,
	pool *pgxpool.Pool,
	client *http.Client,
	from string,
	messageIDDomain string) {
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

	outboxOutputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"outbox_id": {"type": "string", "format": "uuid"},
			"approval_id": {"type": "string", "format": "uuid"},
			"state": {
				"type": "string",
				"enum": ["pending", "sending", "sent", "failed", "needs_attention"]
			},
			"attempts": {"type": "integer", "minimum": 0},
			"rfc822_message_id": {
				"anyOf": [{"type": "string"}, {"type": "null"}]
			},
			"gmail_message_id": {
				"anyOf": [{"type": "string"}, {"type": "null"}]
			},
			"last_error": {
				"anyOf": [{"type": "string"}, {"type": "null"}]
			},
			"claimed_at": {
				"anyOf": [
					{"type": "string", "format": "date-time"},
					{"type": "null"}
				]
			},
			"created_at": {"type": "string", "format": "date-time"},
			"updated_at": {"type": "string", "format": "date-time"}
		},
		"required": [
			"outbox_id", "approval_id", "state", "attempts",
			"rfc822_message_id", "gmail_message_id", "last_error",
			"claimed_at", "created_at", "updated_at"
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

	mcp.AddTool(server, &mcp.Tool{
		Name:         "send_outbox_message",
		Description:  "Send an approved outbox entry and return its current delivery state.",
		OutputSchema: outboxOutputSchema,
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input SendOutboxInput,
	) (*mcp.CallToolResult, OutboxEntry, error) {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()

		entry, err := SendOutboxMessage(ctx, pool, client, input.OutboxID, from, messageIDDomain)
		if err != nil {
			return nil, OutboxEntry{}, err
		}
		return nil, entry, nil
	})
}
