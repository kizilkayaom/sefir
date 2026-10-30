import argparse
import getpass
import json
import re
import subprocess
import sys
import time
from http.cookiejar import CookieJar
from textwrap import dedent
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import (
    HTTPCookieProcessor,
    HTTPRedirectHandler,
    ProxyHandler,
    Request,
    build_opener,
)
from uuid import uuid4


class VerificationError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise VerificationError(message)


def wait_for(check, description, timeout=60):
    deadline = time.monotonic() + timeout
    while True:
        try:
            return check()
        except VerificationError as error:
            if time.monotonic() >= deadline:
                raise VerificationError(f"{description}: {error}") from None
            time.sleep(2)


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class WebClient:
    def __init__(self, origin):
        parsed = urlsplit(origin)
        require(
            parsed.scheme in {"http", "https"}
            and parsed.hostname in {"localhost", "127.0.0.1", "::1"}
            and parsed.username is None
            and parsed.password is None
            and parsed.path in {"", "/"}
            and not parsed.query
            and not parsed.fragment,
            "Use a localhost web origin, for example http://localhost:8080.",
        )
        self.origin = origin.rstrip("/")
        self.port = parsed.port or (443 if parsed.scheme == "https" else 80)
        self.csrf = ""
        self.opener = build_opener(
            ProxyHandler({}), HTTPCookieProcessor(CookieJar()), NoRedirect()
        )

    def request(self, path, payload=None, timeout=25):
        headers = {"Origin": self.origin, "Accept": "application/json"}
        data = None
        if payload is not None:
            data = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
            headers["X-CSRF-Token"] = self.csrf
        request = Request(self.origin + path, data=data, headers=headers)
        try:
            with self.opener.open(request, timeout=timeout) as response:
                result = json.load(response)
        except HTTPError as error:
            raise VerificationError(
                f"{request.get_method()} {path} returned HTTP {error.code}."
            ) from None
        except (URLError, OSError, ValueError):
            raise VerificationError(f"No valid response from {path}.") from None
        require(isinstance(result, dict), f"Invalid response from {path}.")
        return result

    def login(self, username, password):
        session = self.request(
            "/api/login", {"username": username, "password": password}
        )
        require(
            session.get("authenticated")
            and session.get("csrf_token")
            and session.get("account_id"),
            "Password sign-in failed.",
        )
        self.csrf = session["csrf_token"]
        return session["account_id"]

    def items(self, path):
        offset = 0
        while offset <= 1_000_000:
            page = self.request(f"{path}?limit=100&offset={offset}")
            yield from page["items"]
            if not page["has_more"]:
                return
            next_offset = page["next_offset"]
            require(next_offset > offset, f"Pagination stopped advancing for {path}.")
            offset = next_offset
        raise VerificationError(f"Too many records to verify {path}.")


POLLERS = dedent("""\
    import asyncio
    import os
    import socket
    import time
    from temporalio.client import Client
    from temporalio.api.enums.v1 import TaskQueueType
    from temporalio.api.taskqueue.v1 import TaskQueue
    from temporalio.api.workflowservice.v1 import DescribeTaskQueueRequest

    async def check():
        client = await Client.connect(os.environ["TEMPORAL_ADDRESS"])
        for kind in (TaskQueueType.TASK_QUEUE_TYPE_WORKFLOW,
                     TaskQueueType.TASK_QUEUE_TYPE_ACTIVITY):
            response = await client.workflow_service.describe_task_queue(
                DescribeTaskQueueRequest(
                    namespace=client.namespace,
                    task_queue=TaskQueue(name="sefir-outreach"),
                    task_queue_type=kind,
                )
            )
            if not any(
                poller.identity.partition("@")[2] == socket.gethostname()
                and poller.last_access_time.seconds >= time.time() - 90
                for poller in response.pollers
            ):
                raise SystemExit(1)

    asyncio.run(asyncio.wait_for(check(), timeout=20))
    """)

MARKER = dedent("""\
    import os
    import re
    import sys
    from pathlib import Path

    action, identifier = sys.argv[1:]
    if not re.fullmatch(r"[0-9a-f]{32}", identifier):
        raise SystemExit(1)
    path = Path("/data") / (".sefir-verification-" + identifier)
    if action == "create":
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w") as output:
            output.write(identifier)
    elif path.read_text() != identifier:
        raise SystemExit(1)
    elif action == "remove":
        path.unlink()
    """)


class DockerStack:
    def __init__(self, project):
        require(
            re.fullmatch(r"[a-z0-9][a-z0-9_-]*", project),
            "Invalid Compose project name.",
        )
        self.project = project
        self.containers = {}

    def command(self, *arguments, timeout=30):
        try:
            result = subprocess.run(
                ["docker", *arguments],
                check=False,
                capture_output=True,
                text=True,
                timeout=timeout,
            )
        except FileNotFoundError:
            raise VerificationError("Docker CLI is not installed.") from None
        except subprocess.TimeoutExpired:
            raise VerificationError(f"Docker {arguments[0]} timed out.") from None
        require(
            result.returncode == 0,
            f"Docker {arguments[0]} failed. Check Docker access and container status.",
        )
        return result.stdout.strip()

    def discover(self):
        output = self.command(
            "ps",
            "--all",
            "--filter",
            f"label=com.docker.compose.project={self.project}",
            "--filter",
            "label=com.docker.compose.oneoff=False",
            "--format",
            '{{.ID}} {{.Label "com.docker.compose.service"}}',
        )
        for line in output.splitlines():
            identifier, service = line.split()
            if service not in {"web", "worker", "postgres", "temporal", "migrate"}:
                continue
            require(
                service not in self.containers, f"Multiple {service} containers found."
            )
            self.containers[service] = identifier
        for service in ("web", "worker", "postgres", "temporal"):
            require(
                service in self.containers,
                f"No {service} container found for {self.project}; check --project.",
            )

    def inspect(self, service, template):
        return self.command("inspect", "--format", template, self.containers[service])

    def ready(self, web):
        for service in ("web", "worker", "postgres", "temporal"):
            status = self.inspect(service, "{{.State.Status}}")
            require(status == "running", f"{service} is {status}.")
        for service in ("postgres", "temporal"):
            health = self.inspect(
                service, "{{if .State.Health}}{{.State.Health.Status}}{{end}}"
            )
            require(health == "healthy", f"{service} is not healthy.")
        if "migrate" in self.containers:
            status = self.inspect("migrate", "{{.State.Status}} {{.State.ExitCode}}")
            require(status == "exited 0", "Migrations have not completed successfully.")
        ports = json.loads(self.inspect("web", "{{json .NetworkSettings.Ports}}"))
        require(
            any(
                binding["HostPort"] == str(web.port)
                and binding["HostIp"] in {"127.0.0.1", "::1"}
                for binding in ports.get("8080/tcp", []) or []
            ),
            "The web URL must use this project's published localhost port.",
        )
        require(
            web.request("/health", timeout=5).get("status") == "ok",
            "Web is not healthy.",
        )
        require(
            web.request("/api/session").get("signup_available") is False,
            "New account registration is enabled.",
        )

    def shared_volume(self):
        volumes = []
        for service in ("web", "worker"):
            mounts = json.loads(self.inspect(service, "{{json .Mounts}}"))
            data = next(
                (mount for mount in mounts if mount["Destination"] == "/data"), {}
            )
            require(
                data.get("Type") == "volume" and data.get("RW") and data.get("Name"),
                f"{service} needs a writable named volume at /data.",
            )
            volumes.append(data["Name"])
        require(volumes[0] == volumes[1], "Web and worker use different data volumes.")

    def pollers(self):
        self.command("exec", self.containers["worker"], "python", "-c", POLLERS)

    def marker(self, service, action, identifier):
        self.command(
            "exec", self.containers[service], "python", "-c", MARKER, action, identifier
        )

    def restart(self, web):
        identifier = uuid4().hex
        self.marker("web", "create", identifier)
        try:
            self.marker("worker", "read", identifier)
            self.command(
                "restart",
                self.containers["web"],
                self.containers["worker"],
                timeout=120,
            )
            wait_for(lambda: self.ready(web), "Services did not recover after restart")
            wait_for(self.pollers, "Worker did not resume polling Temporal")
            self.marker("web", "read", identifier)
            self.marker("worker", "read", identifier)
        finally:
            try:
                self.marker("web", "remove", identifier)
            except VerificationError:
                print(f"Could not remove /data/.sefir-verification-{identifier}.")


def connected_email(web):
    gmail = web.request("/api/settings/gmail")
    require(
        gmail.get("connected") and gmail.get("email"),
        "Connect Gmail in Settings first.",
    )
    return gmail["email"]


def draft_path(draft):
    return f"/api/drafts/{draft['draft_id']}/versions/{draft['version']}"


def sent_delivery(web, draft, timeout=180):
    deadline = time.monotonic() + timeout
    while True:
        status = web.request(draft_path(draft) + "/status")
        workflow = status.get("workflow") or {}
        delivery = status.get("delivery") or {}
        require(
            workflow.get("status")
            not in {"FAILED", "CANCELED", "TERMINATED", "TIMED_OUT"}
            and delivery.get("state") not in {"failed", "needs_attention"},
            "Delivery needs attention. Inspect Activity before running another email test.",
        )
        if workflow.get("status") == "COMPLETED" and delivery.get("state") == "sent":
            require(
                delivery.get("gmail_message_id") and delivery.get("outbox_id"),
                "Sent delivery is missing its Gmail message ID or outbox ID.",
            )
            return delivery
        require(
            time.monotonic() < deadline,
            "Delivery is not confirmed. Inspect Activity before running another email test.",
        )
        time.sleep(2)


def matching_activity(web, draft):
    entries = [
        item
        for item in web.items("/api/activity")
        if item["draft_id"] == draft["draft_id"] and item["version"] == draft["version"]
    ]
    require(
        len(entries) == 1, "Expected exactly one delivery record for the test draft."
    )
    return entries[0]


def prepare_email(web):
    recipient = connected_email(web)
    contact = web.request(
        "/api/contacts",
        {
            "name": "Sefir Docker verification",
            "email": recipient,
            "organization": "Sefir test workspace",
            "role": "Test recipient",
            "context": "A fictional outreach test sent to the sender's own mailbox.",
            "status": "confirmed",
        },
    )
    draft = web.request(
        "/api/drafts",
        {
            "contact_id": contact["id"],
            "cv_text": (
                "Fictional resume for a Sefir Docker verification email. "
                "Alex Test builds small Python and Go projects using PostgreSQL and Docker. "
                "This is a software test, not a real job application."
            ),
        },
        timeout=150,
    )
    require(
        draft.get("recipient_email") == recipient, "Test draft has the wrong recipient."
    )
    require(draft.get("content_hash"), "Test draft has no approval hash.")
    print(f"Draft: {draft['draft_id']} (version {draft['version']})")
    print(f"To: {recipient}\nSubject: {draft['subject']}\n\n{draft['body']}\n")
    require(
        input("Type SEND to approve this exact draft: ").strip() == "SEND",
        "Email test cancelled; the draft remains saved and was not approved.",
    )
    require(
        web.request(draft_path(draft)) == draft,
        "The reviewed draft changed; test stopped.",
    )
    require(connected_email(web) == recipient, "The connected Gmail account changed.")
    try:
        web.request(
            draft_path(draft) + "/approve", {"content_hash": draft["content_hash"]}
        )
    except VerificationError:
        raise VerificationError(
            "Approval could not be confirmed. Inspect Activity before running another email test."
        ) from None
    delivery = sent_delivery(web, draft)
    require(
        matching_activity(web, draft) == delivery, "Delivery and Activity disagree."
    )
    print(f"PASS: Gmail accepted the test email; outbox {delivery['outbox_id']}.")
    return {
        "contact": contact,
        "draft": draft,
        "delivery": delivery,
        "recipient": recipient,
    }


def check_email_persistence(web, snapshot):
    require(
        connected_email(web) == snapshot["recipient"],
        "Gmail connection did not persist.",
    )
    require(
        web.request(draft_path(snapshot["draft"])) == snapshot["draft"],
        "The saved draft changed after restart.",
    )
    contacts = [
        item
        for item in web.items("/api/contacts")
        if item["id"] == snapshot["contact"]["id"]
    ]
    require(
        contacts == [snapshot["contact"]], "The test contact did not persist unchanged."
    )
    require(
        sent_delivery(web, snapshot["draft"]) == snapshot["delivery"]
        and matching_activity(web, snapshot["draft"]) == snapshot["delivery"],
        "Delivery changed after restart; inspect Activity for additional send attempts.",
    )


def main():
    parser = argparse.ArgumentParser(
        description="Verify a running local Sefir Docker stack."
    )
    parser.add_argument(
        "--project", default="sefir", help="Compose project name (default: sefir)"
    )
    parser.add_argument(
        "--url", default="http://localhost:8080", help="Published localhost web origin"
    )
    parser.add_argument(
        "--restart",
        action="store_true",
        help="Restart web/worker and verify shared storage survives",
    )
    parser.add_argument(
        "--email",
        action="store_true",
        help="Prompt for password login, generate/review/send one test email to yourself, then verify after restart",
    )
    parser.add_argument("--username", help="Password-account username for --email")
    args = parser.parse_args()
    try:
        web = WebClient(args.url)
        stack = DockerStack(args.project)
        stack.discover()
        wait_for(lambda: stack.ready(web), "Stack is not ready")
        stack.shared_volume()
        wait_for(stack.pollers, "Worker is not polling Temporal")
        print(
            "PASS: services, migrations, web health, closed registration, shared volume, and Temporal pollers."
        )
        snapshot = None
        if args.email:
            require(sys.stdin.isatty(), "Run --email in an interactive terminal.")
            username = args.username or input("Username: ").strip()
            password = getpass.getpass("Password: ")
            account_id = web.login(username, password)
            snapshot = prepare_email(web)
        if args.restart or args.email:
            print(
                "Restarting web and worker; browser sessions will need to sign in again.",
                flush=True,
            )
            stack.restart(web)
            print(
                "PASS: web and worker recovered; shared storage survived the restart."
            )
        if snapshot is not None:
            require(
                web.login(username, password) == account_id,
                "Signed into a different account.",
            )
            check_email_persistence(web, snapshot)
            web.request("/api/logout", {})
            print(
                "PASS: contact, draft, Gmail connection, and single delivery record survived unchanged."
            )
            print(
                "Inbox arrival still needs a mailbox check; the app verifies Gmail acceptance."
            )
        return 0
    except (VerificationError, ValueError, KeyError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    except (KeyboardInterrupt, EOFError):
        print(
            "Verification interrupted. Check Activity if you already approved the draft.",
            file=sys.stderr,
        )
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
