# Sefir

Sefir (Turkish for "envoy") is an approval-first outreach agent. It turns phone screenshots into a confirmed catalogue of people and organizations, drafts outreach emails from your CV, and sends each email exactly once, only after you approve it.

Each user deploys and owns a separate instance in their own GCP project.

## How it works

1. You take a screenshot of a profile, post or event page.
2. An ingestion job extracts the person or organization and proposes a catalogue record.
3. You confirm the record. Only confirmed records are searchable.
4. You ask the agent for an email. It drafts one from the record and your CV and lists the facts it used.
5. You approve one exact draft version. The Email MCP server sends it exactly once.
6. Sefir reminds you to follow up if no reply arrives.

Only step 5 changes the outside world, and it always needs your explicit approval. See [docs/design.md](docs/design.md) for the design and the exactly-once outbox.

## Repository layout

| Path | Contents |
|---|---|
| `agent/` | Python uv workspace: `worker/` (ADK agent, Temporal workflows) and `ingestion/` (screenshot extraction job) |
| `mcp/` | Go module: `cmd/catalogue-mcp` and `cmd/email-mcp` |
| `infra/` | Terraform for the GCP environment |
| `docs/` | Design document; later runbooks and postmortems |
| `BACKLOG.md` | Ideas outside the current milestone |

## Local development

Requirements: [uv](https://docs.astral.sh/uv/), Go 1.27+, Terraform 1.16+, the gcloud CLI.

```sh
# Python
cd agent && uv sync --all-packages

# Go
cd mcp && go build ./... && go vet ./...
```

## Deployment

WIP

## License

[Apache-2.0](LICENSE)
