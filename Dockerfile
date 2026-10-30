FROM node:26.13.0-bookworm-slim AS frontend-build

WORKDIR /build/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM golang:1.27.3-bookworm AS go-build

WORKDIR /build/mcp
ENV CGO_ENABLED=0

COPY mcp/go.mod mcp/go.sum ./
RUN go mod download
RUN GOBIN=/out go install -ldflags="-X main.version=v3.29.0" \
    github.com/pressly/goose/v3/cmd/goose@v3.29.0

COPY mcp/ ./

RUN go build -mod=readonly -trimpath -o /out/sefir-web ./cmd/web
RUN go build -mod=readonly -trimpath -o /out/email-mcp ./cmd/email-mcp
RUN go build -mod=readonly -trimpath -o /out/catalogue-mcp ./cmd/catalogue-mcp

FROM python:3.13.18-slim-bookworm AS python-build

COPY --from=ghcr.io/astral-sh/uv:0.11.17 /uv /usr/local/bin/uv

WORKDIR /build/agent

ENV UV_PROJECT_ENVIRONMENT=/opt/venv \
    UV_PYTHON_DOWNLOADS=never

COPY agent/ ./

RUN uv sync --locked --no-dev --no-editable --package sefir-agent --no-cache

FROM python:3.13.18-slim-bookworm AS runtime

RUN groupadd --gid 10001 sefir \
    && useradd --uid 10001 --gid sefir --create-home sefir \
    && install -d -m 0700 -o sefir -g sefir /data

COPY --from=python-build /opt/venv /opt/venv
COPY --from=go-build /out/sefir-web /usr/local/bin/sefir-web
COPY --from=go-build /out/email-mcp /usr/local/bin/email-mcp
COPY --from=go-build /out/catalogue-mcp /usr/local/bin/catalogue-mcp
COPY --from=frontend-build /build/web/dist /opt/sefir/web
COPY --from=go-build /out/goose /usr/local/bin/goose
COPY db/migrations/ /opt/sefir/migrations/

ENV PATH="/opt/venv/bin:${PATH}" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    SEFIR_ROOT=/data \
    SEFIR_WEB_DIST=/opt/sefir/web \
    SEFIR_PYTHON=/opt/venv/bin/python \
    SEFIR_EMAIL_MCP_BIN=/usr/local/bin/email-mcp \
    SEFIR_CATALOGUE_MCP_BIN=/usr/local/bin/catalogue-mcp \
    SEFIR_LISTEN_ADDR=0.0.0.0:8080 \
    SEFIR_SIGNUP_ENABLED=false

WORKDIR /data
USER sefir

EXPOSE 8080

CMD ["/usr/local/bin/sefir-web"]