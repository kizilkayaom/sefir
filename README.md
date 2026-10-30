# Sefir

Sefir (Turkish for "envoy") is an approval-first outreach agent. It drafts outreach emails from confirmed contacts and your résumé, then sends them through Gmail after you approve an exact saved version. Temporal and a PostgreSQL outbox track delivery and recovery.

The web app gives each account a private workspace, with a project landing page and password or Google sign-in. Sefir is built for self-hosting, with a public Apache-2.0 source release planned. Registration on the current instance is closed; existing accounts can still sign in. AWS deployment is planned.

## How it works

1. Add a contact and confirm its details.
2. Choose a confirmed contact and either write an email or provide your résumé as plain text.
3. Write with the rich-text editor or choose Cohere, Anthropic, OpenAI, or OpenRouter to generate a starting draft. Edits save as new versions for review.
4. Review the recipient, subject, body, and exact version.
5. Approve sending. Temporal records approval and delivers through the existing Email MCP and outbox.
6. Check workflow status, delivery results, or uncertain sends in the web app.

Draft generation calls the model; sending requires a separate approval. Screenshot ingestion, retrieval, and reply tracking are later milestones. A sent state means Gmail accepted the message, not that it reached the recipient's inbox.

Start with [Docker Compose](#run-with-docker-compose), then configure [Google sign-in and Gmail](#google-sign-in-and-gmail). Instructions for [local development](#local-development) and [tests](#tests) are below.

## Run with Docker Compose

Requirements: Docker with the Compose plugin and a running Docker daemon. AI drafting needs an API key from Cohere, Anthropic, OpenAI, or OpenRouter; writing emails manually does not. Python 3.10+ is needed on your host to run the verification script. The application builds its Go, Python, and frontend dependencies inside Docker.

This setup runs on localhost and uses Temporal's development server with persistent storage. It is intended for local use; production deployment is a later milestone.

Run these steps from the repository root:

1. Copy the example configuration. This command keeps an existing `.env` file:

   ```sh
   cp -n .env.example .env
   ```

2. Edit `.env` and set these three values for a fresh installation:

   | Variable | Purpose |
   |---|---|
   | `POSTGRES_PASSWORD` | A password for the application's PostgreSQL user |
   | `SEFIR_OWNER` | The username for your first account |
   | `SEFIR_OWNER_PASSWORD` | The password for your first account |

   For AI drafting, add a key later in **Settings → AI providers**, or set at least one of `COHERE_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `OPENROUTER_API_KEY` here. Leave unused provider keys blank. See [AI providers](#ai-providers) for model selection and operator access.

   Usernames must be 3–32 ASCII letters, numbers, dots, underscores, or hyphens. Account passwords must be 8–128 characters and include a letter, a number, and a symbol.

   Both owner values are needed to create the first account on a fresh database while public registration remains closed. Existing accounts are stored in PostgreSQL; established installations can leave both owner values empty. Changing these values after the account has been created does not reset its username or password.

   Keep real credentials in the ignored `.env` file. Single-quote values containing `$` to preserve them literally; see [Docker's environment file syntax](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/#env-file-syntax).

3. Validate the configuration and start the application:

   ```sh
   docker compose config --quiet
   docker compose up -d --build web worker
   ```

   Compose starts PostgreSQL and Temporal and applies database migrations before starting web and worker. The named volumes retain database records, Temporal history, and application files across container recreation.

4. Open **http://localhost:8080** and sign in with the owner username and password you configured. Verify the running services:

   ```sh
   python3 scripts/verify_docker.py
   ```

5. To enable Google sign-in and Gmail sending, follow [Google sign-in and Gmail](#google-sign-in-and-gmail). For restart checks and the email test, see [Verify the Docker installation](#verify-the-docker-installation).

## AI providers

The installation's operator can add, replace, or remove shared keys in **Settings → AI providers**, select models from searchable lists, and choose a default provider. Other accounts see a read-only list and choose among configured providers on **New draft**. Keys are not stored in account records or returned to the browser. All AI usage is billed to the operator's provider accounts.

To configure a provider, paste its API key and choose **Save**. The settings form then fetches available models using the saved key. Search by model name or ID, select a model, and save again to use it. A blank key field keeps the existing key when editing a configured provider. Changes apply to the next web drafting request without restarting the server.

Model discovery runs on the server and caches each provider's list for ten minutes. **Refresh models** bypasses that cache, and replacing a key invalidates the previous key's list. Refreshing never changes your selected model, including when it disappears from the catalogue. **Enter model ID manually** remains available for new models, restricted model-list permissions, and temporary discovery failures. Discovery sends no résumé or contact data and performs no text generation; successful listing does not verify inference permissions, billing, or drafting compatibility.

Cohere discovery requests chat models and removes deprecated entries. Anthropic discovery removes models marked deprecated or retired. OpenAI discovery intersects the API catalogue with the installed LiteLLM package's chat-model metadata, excluding specialized audio, search, computer-use, cybersecurity, and research models, and models whose reported shutdown date has passed. Updating the worker dependencies can expand the recognized OpenAI list; manual IDs remain possible without an update. The discovery endpoints are documented by [OpenAI](https://developers.openai.com/api/reference/resources/models/methods/list), [Anthropic](https://platform.claude.com/docs/en/api/models/list), and [Cohere](https://docs.cohere.com/v2/reference/list-models).

OpenRouter discovery uses its [account-filtered model catalogue](https://openrouter.ai/docs/api/api-reference/models/list-models-filtered-by-user-provider-preferences-privacy-settings-and-guardrails), follows pagination, and keeps models accepting text input with text-only output. Expired models are omitted. The catalogue reflects the OpenRouter account's provider preferences, privacy settings, and guardrails; use **Refresh models** after changing those settings. OpenRouter requires only an OpenRouter key, without separate keys for the underlying model providers. Drafting sends your résumé and contact details through OpenRouter to the provider serving the selected model.

UI settings are stored in `SEFIR_ROOT/.sefir/ai-settings.json`, with file permissions `0600` inside a `0700` directory. The file contains keys in plaintext and is protected by filesystem permissions; it is excluded from Git and Docker build contexts. Docker retains it in the existing `sefir_data` volume. Saved settings override the server environment for web drafting. **Remove key and disable** deletes the saved key and prevents web drafting from using an environment key for that provider. Removing the default provider restores automatic selection.

Operator access is tied to an account UUID, not a username or Google email. By default, it belongs to the original account bootstrapped with `SEFIR_OWNER` and `SEFIR_OWNER_PASSWORD` (the legacy account UUID `00000000-0000-4000-8000-000000000001`). To designate a different existing account, set `SEFIR_OPERATOR_ACCOUNT_ID` to its UUID and restart or recreate the web service. An authenticated user can find their own UUID in `/api/session`'s `account_id` field. New registrations do not receive operator access.

Environment-based configuration remains available:

| Provider | API key variable | Default model | Optional model override |
|---|---|---|---|
| Cohere | `COHERE_API_KEY` | `command-a-plus-05-2026` | `SEFIR_COHERE_MODEL` |
| Anthropic (Claude) | `ANTHROPIC_API_KEY` | `claude-sonnet-5-5` | `SEFIR_ANTHROPIC_MODEL` |
| OpenAI (GPT) | `OPENAI_API_KEY` | `gpt-6-luna` | `SEFIR_OPENAI_MODEL` |
| OpenRouter | `OPENROUTER_API_KEY` | `openai/gpt-6-luna` | `SEFIR_OPENROUTER_MODEL` |

Automatic selection uses the first configured provider in this order: Cohere, Anthropic, OpenAI, OpenRouter. Choose a default in Settings, or set `SEFIR_DEFAULT_PROVIDER` to `cohere`, `anthropic`, `openai`, or `openrouter` when using environment configuration. It must have a configured API key. Any provider can be used on its own.

Direct Cohere, Anthropic, and OpenAI overrides take a bare model ID, without a provider prefix or URL. OpenRouter uses its full `publisher/model` ID, such as `openai/gpt-6-luna`; optional variants such as `vendor/model:free` are accepted. Do not prepend an extra `openrouter/` to the ID from its catalogue. Choose a text-generation model your API account can access. Defaults are listed in the [OpenAI model catalogue](https://developers.openai.com/api/docs/models), [Anthropic model catalogue](https://platform.claude.com/docs/en/models/overview), and [OpenRouter's GPT-6 Luna page](https://openrouter.ai/openai/gpt-6-luna). Model access and API billing are separate from signing into consumer chat apps.

UI saves take effect immediately for subsequent requests. Restart the local web server after changing its environment. For Docker environment changes, recreate the web service; use both Compose files if Google is enabled:

```sh
docker compose up -d web
```

The Temporal delivery worker does not use AI keys. The web process performs generation through the Python bridge, using the current saved settings and environment.

Generation sends the supplied résumé and selected contact details to the chosen provider. A failed request does not switch to another provider. All providers return the same subject/body draft for review, and sending still requires approval of the exact saved version. With no keys configured, manual drafting remains available.

The standalone CLI uses environment configuration; it does not read the web settings file. Add `--provider cohere`, `--provider anthropic`, `--provider openai`, or `--provider openrouter` to `sefir-agent draft`; omitting it uses the environment's default. The Docker email verification script calls the web API and therefore uses the web settings, including keys saved through the UI.

## Google sign-in and Gmail

Use a Google OAuth **Web application** client. Enable the Gmail API in the same Google Cloud project, configure the OAuth consent screen, and add your Google account as a test user if the app is in testing mode. Download the client configuration to `credentials-gmail.json` in the repository root. This file is excluded from Git and the Docker build context.

Register both exact **Authorized redirect URIs** on the client:

```text
http://localhost:8080/oauth/google/sign-in/callback
http://localhost:8080/oauth/google/callback
```

The first callback is for account sign-in; the second is for permission to send email. For a different web origin, replace the origin in both URLs and configure `SEFIR_WEB_ORIGIN` to match.

Registration is closed by default. To add Google sign-in to an existing password account, sign in with your password and choose **account name → Settings → Link Google account**. Once linked, **Sign in with Google** returns you to that workspace. An unregistered Google identity cannot create an account while registration is closed.

Separately, choose **Settings → Connect Gmail** to authorize sending from your mailbox. Each account connects its own mailbox. Google sign-in alone does not authorize Gmail access. Password sign-in remains available without Google configuration.

### Google with Docker Compose

The optional [compose.google.yaml](compose.google.yaml) mounts `credentials-gmail.json` read-only into both web and worker at `/data/credentials-gmail.json`. The file must exist and be readable by the container user (UID 10001).

Run from the repository root:

```sh
docker compose -f compose.yaml -f compose.google.yaml config --quiet
docker compose -f compose.yaml -f compose.google.yaml up -d web worker
```

Use both `-f` options for subsequent Compose commands that recreate these services. The override retains the shared `sefir_data` volume and closed registration.

Connect Gmail through Settings after starting the containers. Host token files are not automatically imported into Docker; reconnecting saves authorization in the shared `/data` directory. Keep the `sefir_data` volume to preserve it across container recreation.

### Google during local development

Restart the local web server after saving the client configuration. The original account uses the root `token-gmail.json`; other accounts use `.sefir/accounts/<account-id>/token-gmail.json`. Web and worker must use the same `SEFIR_ROOT` so they share these files. Both locations are ignored by Git.

For Vite development at `http://localhost:5173`, register both callbacks with that origin as well and set the web server's `SEFIR_WEB_ORIGIN` accordingly.

## Verify the Docker installation

Run from the repository root with Python 3.10+ and access to the Docker daemon:

```sh
python3 scripts/verify_docker.py
```

The default check verifies the containers, migrations when the migration container exists, PostgreSQL and Temporal health, the web endpoint, closed registration, shared storage, and worker pollers. It does not load `.env` or open Google credentials or tokens. Use `--project NAME` or `--url http://localhost:PORT` for a different Compose project or localhost port.

To check shared storage across a restart:

```sh
python3 scripts/verify_docker.py --restart
```

This creates a temporary marker under `/data`, checks it from both containers, restarts web and worker, checks readiness and persistence, then removes the marker. Sign in again afterward because restarting the web server invalidates sessions.

For an end-to-end email check:

```sh
python3 scripts/verify_docker.py --email
```

This mode requires an interactive terminal and an existing password account with Gmail connected. It prompts for your login, creates a test contact using your connected mailbox, and calls the default AI provider to draft an email from a fictional résumé. It displays the draft and requires typing `SEND` before sending a real email to that mailbox. The test contact, draft, and delivery record remain in your workspace.

After Gmail accepts the message, the script restarts web and worker and checks that the account, Gmail connection, and single delivery record persist with the same outbox ID, Gmail message ID, and attempt count. Uncertain approvals or delivery failures stop the check without another approval attempt. Confirm inbox arrival separately; Gmail acceptance does not prove delivery. Google-only accounts can use the default and `--restart` checks.

## Repository layout

| Path | Contents |
|---|---|
| `agent/` | Python uv workspace: `worker/` (ADK agent, Temporal workflows) and `ingestion/` (reserved for document and screenshot ingestion) |
| `mcp/` | Go catalogue/email tools, Gmail authorization, and the web HTTP server |
| `web/` | React interface for contacts, drafts, approval, activity, and settings |
| `infra/` | Terraform for AWS storage |
| `db/migrations/` | PostgreSQL schema migrations |
| `scripts/` | Docker installation verification and its unit tests |

## Local development

Requirements: [uv](https://docs.astral.sh/uv/), Go 1.27+, Node 26, PostgreSQL with pgvector, Temporal, and Goose for migrations. Terraform and AWS tooling are used for infrastructure work.

Run all commands from the repository root. Start PostgreSQL and Temporal before the web server and worker. For a local Temporal development server, run `temporal server start-dev` in its own terminal. The Docker Compose Temporal service is internal to Docker, so host processes need a separate reachable Temporal server.

1. Install the Python workspace and frontend dependencies:

   ```sh
   uv sync --project agent --all-packages --locked
   npm --prefix web ci
   ```

2. Set `DATABASE_URL` and any desired [AI provider keys](#ai-providers) in your ignored `.env`. The URL must identify your running PostgreSQL database; the Compose example does not set a host-side database URL. If the URL omits the password, also set `PGPASSWORD`. Set both `SEFIR_OWNER` and `SEFIR_OWNER_PASSWORD` to bootstrap the first account, as described in the Docker setup.

   Existing accounts do not need new owner values. When upgrading from the original single-account setup, keep the original credentials for the first startup after migration so the account can be imported. Later changes to those environment variables do not replace stored credentials.

3. Apply all database migrations:

   ```sh
   uv run --project agent --locked --env-file .env sh -c \
     'GOOSE_DRIVER=postgres GOOSE_DBSTRING="${DATABASE_URL:?Set DATABASE_URL}" goose -env=none -dir db/migrations up'
   ```

4. Build the frontend:

   ```sh
   npm --prefix web run build
   ```

5. Start the worker in one terminal:

   ```sh
   uv run --project agent --package sefir-agent --locked --env-file .env sefir-worker
   ```

6. Start the web server in another terminal:

   ```sh
   uv run --project agent --locked --env-file .env go -C mcp run ./cmd/web
   ```

Open **http://localhost:8080** and sign in. The local web server needs port 8080; stop a Docker web container or standalone `gmail-auth` helper already using that port before starting it.

### Configuration

These defaults apply to local development; Compose supplies its own container paths and Temporal address.

| Variable | Default or purpose |
|---|---|
| `DATABASE_URL` | Required PostgreSQL connection string |
| `PGPASSWORD` | Database password when omitted from the URL |
| `COHERE_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY` | At least one for AI drafting; unused keys can be blank |
| `SEFIR_DEFAULT_PROVIDER` | First configured provider; see [AI providers](#ai-providers) |
| `SEFIR_OPERATOR_ACCOUNT_ID` | Original bootstrapped account UUID; controls who can edit shared AI settings |
| `SEFIR_WEB_ORIGIN` | `http://localhost:8080` |
| `SEFIR_LISTEN_ADDR` | `127.0.0.1:8080` |
| `SEFIR_ROOT` | Repository root with the commands above |
| `SEFIR_WEB_DIST` | `web/dist` under `SEFIR_ROOT` |
| `SEFIR_PYTHON` | `agent/.venv/bin/python` under `SEFIR_ROOT` |
| `TEMPORAL_ADDRESS` | `127.0.0.1:7233`; use the same value for web and worker |
| `SEFIR_SIGNUP_ENABLED` | `false`; Compose also explicitly sets it to `false` |

Vite does not load environment files. Keep database, model, and Google credentials in the server environment and ignored local files. The local CLI and stdio MCP tools are trusted operator interfaces with database access; account isolation is enforced at the web boundary.

For hot reload, start Vite with `npm --prefix web run dev -- --host localhost`, set the web server's `SEFIR_WEB_ORIGIN` to `http://localhost:5173`, and restart the web server. Open that origin in your browser. Vite proxies API and OAuth requests to port 8080.

### Compiled MCP executables

Local development uses `go run` for MCP tools by default. To use compiled binaries, build them into the ignored `mcp/bin/` directory:

```sh
go -C mcp build -o bin/email-mcp ./cmd/email-mcp
go -C mcp build -o bin/catalogue-mcp ./cmd/catalogue-mcp
```

Set `SEFIR_EMAIL_MCP_BIN` and `SEFIR_CATALOGUE_MCP_BIN` to their absolute paths in the calling process's environment. Supply `DATABASE_URL` and, if needed, `PGPASSWORD`; set `SEFIR_ROOT` to the directory shared with the web server for Gmail files. The compiled tools inherit these selected settings and do not load `.env` themselves. Docker already uses compiled executables.

## Tests

Run from the repository root:

```sh
npm --prefix web run lint
npm --prefix web run build
npm --prefix web exec -- playwright install chromium
npm --prefix web test
go -C mcp test -race ./...
uv run --project agent --package sefir-agent --locked python -m unittest discover -s agent/worker/tests -v
python3 -m unittest discover -s scripts/tests -v
```

Browser tests use mocked APIs, and the Python unit tests do not contact AI providers, Gmail, or a real Temporal server. Drafting tests exercise the ADK/LiteLLM adapter with mocked model responses for all four providers. The Docker verification script's unit tests also use fake responses and never send email.

Database integration tests require a separate `sefir_test` database. Set `TEST_DATABASE_URL` to its connection string and apply migrations before running the Go tests:

```sh
uv run --project agent --locked --env-file .env sh -c \
  'GOOSE_DRIVER=postgres GOOSE_DBSTRING="${TEST_DATABASE_URL:?Set TEST_DATABASE_URL}" goose -env=none -dir db/migrations up'
uv run --project agent --locked --env-file .env go -C mcp test -race -count=1 ./...
```

With the Temporal CLI installed, run the workflow integration tests:

```sh
uv run --project agent --package sefir-agent --locked python -m unittest discover -s agent/worker/integration_tests -v
```

GitHub Actions runs frontend lint, builds and browser tests, Go database tests, Python unit and Temporal integration tests, and Terraform validation.

## Troubleshooting

- **Web assets are missing:** Run `npm --prefix web run build`, or set `SEFIR_WEB_DIST` to the directory containing the built `index.html`.
- **Port 8080 is in use:** Stop the other web server or use a different listen address and matching web origin.
- **Unexpected request host/origin:** Open the exact configured origin, including hostname and port. `localhost` and `127.0.0.1` are different origins.
- **Database operation failed:** Check that PostgreSQL is running and all migrations are applied, then restart the web server and worker.
- **Draft generation failed:** Check that the Python workspace is installed, the web server inherited the selected provider's API key, the model is accessible with that key, and `SEFIR_PYTHON` points to the workspace interpreter.
- **AI providers could not be loaded:** Check that `SEFIR_DEFAULT_PROVIDER` names a configured provider. Direct providers need bare model IDs; OpenRouter needs a `publisher/model` ID. Restart or recreate the web service after changing its environment.
- **AI settings show “Endpoint not found”:** Restart the Go web server after updating the code, or rebuild and recreate the Docker web service. Rebuilding the frontend alone does not add backend routes.
- **AI settings have no key-editing fields:** Sign in as the original owner, or explicitly designate your account using `SEFIR_OPERATOR_ACCOUNT_ID` and restart the web service.
- **Temporal unavailable or approval still waiting:** Check the Temporal address and that the worker is polling `sefir-outreach`.
- **Google redirect mismatch:** Register the exact callback URL for the origin you are using, including its path and port.
- **Gmail reconnect required:** Reconnect in Settings. Check that the Gmail API is enabled and its callback URL is registered.
- **Old data missing after Google sign-in:** Sign in with the original account. A separate Google account has its own workspace; link Google from Settings to use it with an existing password account.

## Deployment

For local installation, follow [Run with Docker Compose](#run-with-docker-compose). For development outside Docker, follow [Local development](#local-development). AWS deployment is planned; the Terraform configuration currently covers storage. A remote web origin requires HTTPS and a reverse proxy that preserves the public Host header.

## License

[Apache-2.0](LICENSE)
