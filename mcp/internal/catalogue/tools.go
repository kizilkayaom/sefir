package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetConfirmedContactInput struct {
	ContactID string `json:"contact_id" jsonschema:"UUID of the confirmed contact to retrieve"`
}

type GetDraftInput struct {
	DraftID string `json:"draft_id"`
	Version int    `json:"version"`
}

func RegisterTools(server *mcp.Server, pool *pgxpool.Pool) {
	draftOutputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"draft_id": {"type": "string", "format": "uuid"},
			"version": {"type": "integer", "minimum": 1},
			"contact_id": {"type": "string", "format": "uuid"},
			"recipient_email": {
				"anyOf": [{"type": "string"}, {"type": "null"}]
			},
			"subject": {"type": "string", "minLength": 1},
			"body": {"type": "string", "minLength": 1},
			"body_html": {"type": "string"},
			"content_hash": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
			"created_at": {"type": "string", "format": "date-time"}
		},
		"required": [
			"draft_id", "version", "contact_id", "recipient_email",
			"subject", "body", "content_hash", "created_at"
		],
		"additionalProperties": false
	}`)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_confirmed_contact",
		Description: "Retrieve a confirmed contact by its UUID.",
		OutputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id": {"type": "string"},
				"name": {
					"anyOf": [{"type": "string"}, {"type": "null"}]
				},
				"email": {
					"anyOf": [{"type": "string"}, {"type": "null"}]
				},
				"organization": {
					"anyOf": [{"type": "string"}, {"type": "null"}]
				},
				"role": {
					"anyOf": [{"type": "string"}, {"type": "null"}]
				},
				"context": {
					"anyOf": [{"type": "string"}, {"type": "null"}]
				}
			},
			"required": ["id", "name", "email", "organization", "role", "context"],
			"additionalProperties": false
		}`),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input GetConfirmedContactInput,
	) (*mcp.CallToolResult, Contact, error) {
		if strings.TrimSpace(input.ContactID) == "" {
			return nil, Contact{}, errors.New("contact_id is required")
		}

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		contact, err := GetConfirmedContact(ctx, pool, input.ContactID)

		if err != nil {
			return nil, Contact{}, err
		} else {
			return nil, contact, nil
		}
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:         "create_draft",
		Description:  "Save a new outreach draft for a confirmed contact for later human review.",
		OutputSchema: draftOutputSchema,
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input CreateDraftInput,
	) (*mcp.CallToolResult, Draft, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		draft, err := CreateDraft(ctx, pool, input)
		if err != nil {
			return nil, Draft{}, err
		} else {
			return nil, draft, nil
		}
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:         "get_draft",
		Description:  "Retrieve a saved outreach draft by its UUID and version.",
		OutputSchema: draftOutputSchema,
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input GetDraftInput,
	) (*mcp.CallToolResult, Draft, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		draft, err := GetDraft(ctx, pool, input.DraftID, input.Version)
		if err != nil {
			return nil, Draft{}, err
		}
		return nil, draft, nil
	})
}
