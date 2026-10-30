import copy
import io
import unittest
from contextlib import redirect_stdout
from unittest.mock import Mock, patch

from scripts import verify_docker as verify


class FakeWeb:
    def __init__(self):
        self.contact = {"id": "contact-test", "email": "owner@example.com"}
        self.draft = {
            "draft_id": "draft-test",
            "version": 1,
            "recipient_email": "owner@example.com",
            "subject": "Docker test",
            "body": "Fictional test email.",
            "content_hash": "hash-test",
        }
        self.saved_draft = copy.deepcopy(self.draft)
        self.delivery = {
            "draft_id": "draft-test",
            "version": 1,
            "outbox_id": "outbox-test",
            "gmail_message_id": "gmail-test",
            "state": "sent",
            "attempts": 1,
        }
        self.approvals = []
        self.approval_error = False
        self.workflow_status = "COMPLETED"

    def request(self, path, payload=None, timeout=25):
        if path == "/api/settings/gmail":
            return {"connected": True, "email": "owner@example.com"}
        if path == "/api/contacts":
            return copy.deepcopy(self.contact)
        if path == "/api/drafts":
            return copy.deepcopy(self.draft)
        if path.endswith("/approve"):
            self.approvals.append(payload)
            if self.approval_error:
                raise verify.VerificationError("Connection lost")
            return {"workflow": {"status": "RUNNING"}}
        if path.endswith("/status"):
            return {
                "workflow": {"status": self.workflow_status},
                "delivery": copy.deepcopy(self.delivery),
            }
        return copy.deepcopy(self.saved_draft)

    def items(self, path):
        return [
            copy.deepcopy(self.contact if path == "/api/contacts" else self.delivery)
        ]


class DockerVerificationTests(unittest.TestCase):
    def test_default_mode_does_not_restart_or_send_email(self):
        with (
            patch.object(verify.sys, "argv", ["verify_docker.py"]),
            patch.object(verify, "DockerStack") as stack_class,
            patch.object(verify, "WebClient") as web_class,
            redirect_stdout(io.StringIO()),
        ):
            self.assertEqual(verify.main(), 0)

        stack_class.return_value.restart.assert_not_called()
        web_class.return_value.login.assert_not_called()

    def test_remote_or_credential_bearing_origins_are_rejected(self):
        for origin in (
            "https://example.com",
            "http://owner:password@localhost:8080",
            "http://localhost:8080/path",
            "http://localhost:8080?token=test",
        ):
            with (
                self.subTest(origin=origin),
                self.assertRaises(verify.VerificationError),
            ):
                verify.WebClient(origin)

    def test_client_does_not_follow_redirects(self):
        self.assertIsNone(
            verify.NoRedirect().redirect_request(
                None, None, 307, "Redirect", {}, "https://example.com"
            )
        )

    def test_client_sends_origin_and_csrf(self):
        client = verify.WebClient("http://localhost:8080")
        client.csrf = "csrf-test"
        response = Mock()
        response.read.return_value = b'{"ok":true}'
        client.opener = Mock()
        client.opener.open.return_value.__enter__ = Mock(return_value=response)
        client.opener.open.return_value.__exit__ = Mock(return_value=False)

        self.assertEqual(client.request("/api/example", {"value": 1}), {"ok": True})

        request = client.opener.open.call_args.args[0]
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(request.get_header("Origin"), client.origin)
        self.assertEqual(request.get_header("X-csrf-token"), "csrf-test")

    def test_cancelled_review_never_approves(self):
        web = FakeWeb()
        with (
            patch("builtins.input", return_value="no"),
            redirect_stdout(io.StringIO()),
            self.assertRaisesRegex(verify.VerificationError, "cancelled"),
        ):
            verify.prepare_email(web)
        self.assertEqual(web.approvals, [])

    def test_changed_recipient_or_content_stops_approval(self):
        for change in ("recipient", "content"):
            web = FakeWeb()
            if change == "recipient":
                web.draft["recipient_email"] = "someone-else@example.com"
            else:
                web.saved_draft["body"] = "Changed after review"
            with (
                self.subTest(change=change),
                patch("builtins.input", return_value="SEND"),
                redirect_stdout(io.StringIO()),
                self.assertRaises(verify.VerificationError),
            ):
                verify.prepare_email(web)
            self.assertEqual(web.approvals, [])

    def test_uncertain_approval_is_not_retried(self):
        web = FakeWeb()
        web.approval_error = True
        with (
            patch("builtins.input", return_value="SEND"),
            redirect_stdout(io.StringIO()),
            self.assertRaisesRegex(verify.VerificationError, "Inspect Activity"),
        ):
            verify.prepare_email(web)
        self.assertEqual(web.approvals, [{"content_hash": "hash-test"}])

    def test_email_verification_approves_once_and_checks_persistence(self):
        web = FakeWeb()
        with (
            patch("builtins.input", return_value="SEND"),
            redirect_stdout(io.StringIO()),
        ):
            snapshot = verify.prepare_email(web)
        verify.check_email_persistence(web, snapshot)
        self.assertEqual(web.approvals, [{"content_hash": "hash-test"}])

    def test_extra_delivery_attempt_after_restart_fails(self):
        web = FakeWeb()
        with (
            patch("builtins.input", return_value="SEND"),
            redirect_stdout(io.StringIO()),
        ):
            snapshot = verify.prepare_email(web)
        web.delivery["attempts"] = 2
        with self.assertRaisesRegex(verify.VerificationError, "Delivery changed"):
            verify.check_email_persistence(web, snapshot)

    def test_duplicate_activity_is_not_reported_as_success(self):
        web = FakeWeb()
        web.items = Mock(return_value=[web.delivery, web.delivery])
        with self.assertRaisesRegex(verify.VerificationError, "exactly one"):
            verify.matching_activity(web, web.draft)

    def test_failed_or_uncertain_delivery_stops_polling(self):
        for state in ("failed", "needs_attention"):
            web = FakeWeb()
            web.delivery["state"] = state
            web.workflow_status = "RUNNING"
            with (
                self.subTest(state=state),
                self.assertRaisesRegex(verify.VerificationError, "needs attention"),
            ):
                verify.sent_delivery(web, web.draft)

    def test_delivery_timeout_does_not_submit_another_approval(self):
        web = FakeWeb()
        web.delivery["state"] = "pending"
        web.workflow_status = "RUNNING"
        with self.assertRaisesRegex(verify.VerificationError, "not confirmed"):
            verify.sent_delivery(web, web.draft, timeout=0)
        self.assertEqual(web.approvals, [])

    def test_restart_failure_still_removes_only_its_marker(self):
        stack = verify.DockerStack("sefir")
        stack.containers = {"web": "web-test", "worker": "worker-test"}
        stack.marker = Mock()
        stack.command = Mock(side_effect=verify.VerificationError("Restart failed"))
        with self.assertRaisesRegex(verify.VerificationError, "Restart failed"):
            stack.restart(Mock())
        created = stack.marker.call_args_list[0].args
        self.assertEqual(created[:2], ("web", "create"))
        stack.marker.assert_any_call("worker", "read", created[2])
        stack.marker.assert_called_with("web", "remove", created[2])

    def test_docker_error_does_not_print_subprocess_output(self):
        stack = verify.DockerStack("sefir")
        with (
            patch.object(
                verify.subprocess,
                "run",
                return_value=Mock(returncode=1, stdout="private", stderr="private"),
            ),
            self.assertRaises(verify.VerificationError) as raised,
        ):
            stack.command("exec", "worker-test", "python", "-c", "pass")
        self.assertNotIn("private", str(raised.exception))
