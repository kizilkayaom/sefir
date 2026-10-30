package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SendOutboxInput struct {
	OutboxID string `json:"outbox_id"`
}

type RecoverOutboxInput struct {
	OlderThanMinutes int `json:"older_than_minutes"`
	Limit            int `json:"limit"`
}

type RecoverOutboxResult struct {
	Entries []OutboxEntry `json:"entries"`
	Error   string        `json:"error"`
}

func RegisterTools(server *mcp.Server,
	pool *pgxpool.Pool,
	client *http.Client,
	from string,
	messageIDDomain string) {
	RegisterAccountTools(server, pool, func(context.Context, string) (DeliveryAccount, error) {
		return DeliveryAccount{Client: client, From: from, MessageIDDomain: messageIDDomain}, nil
	})
}

func RegisterAccountTools(server *mcp.Server, pool *pgxpool.Pool, resolve DeliveryAccountResolver) {
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

		account, err := resolve(ctx, input.OutboxID)
		if err != nil {
			return nil, OutboxEntry{}, err
		}
		entry, err := SendOutboxMessage(ctx, pool, account.Client, input.OutboxID, account.From, account.MessageIDDomain)
		if err != nil {
			return nil, OutboxEntry{}, err
		}
		return nil, entry, nil
	})

	recoveryInputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"older_than_minutes": {"type": "integer", "minimum": 1, "maximum": 10080},
			"limit": {"type": "integer", "minimum": 1, "maximum": 100}
		},
		"required": ["older_than_minutes", "limit"],
		"additionalProperties": false
	}`)

	recoveryOutputSchema := json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"entries": {"type": "array", "items": %s},
			"error": {"type": "string"}
		},
		"required": ["entries", "error"],
		"additionalProperties": false
	}`, outboxOutputSchema))

	mcp.AddTool(server, &mcp.Tool{
		Name:         "recover_stale_outbox_entries",
		Description:  "Check stale sending entries against Gmail and record confirmed matches.",
		InputSchema:  recoveryInputSchema,
		OutputSchema: recoveryOutputSchema,
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input RecoverOutboxInput,
	) (*mcp.CallToolResult, RecoverOutboxResult, error) {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()

		cutoff := time.Now().Add(-time.Duration(input.OlderThanMinutes) * time.Minute)
		entries, err := RecoverStaleOutboxEntriesWithClients(ctx, pool, func(ctx context.Context, id string) (*http.Client, error) {
			account, err := resolve(ctx, id)
			return account.Client, err
		}, cutoff, input.Limit)
		if entries == nil {
			entries = []OutboxEntry{}
		}

		result := RecoverOutboxResult{Entries: entries}
		if err != nil {
			result.Error = err.Error()
		}
		return nil, result, nil
	})
}
