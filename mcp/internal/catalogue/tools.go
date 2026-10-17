package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"time"
)

type GetConfirmedContactInput struct {
	ContactID string `json:"contact_id" jsonschema:"UUID of the confirmed contact to retrieve"`
}

func RegisterTools(server *mcp.Server, pool *pgxpool.Pool) {
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
}
