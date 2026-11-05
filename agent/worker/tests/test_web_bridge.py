import ast
import os
import sys
import unittest
from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace
from typing import Any
from unittest.mock import AsyncMock, MagicMock, call, patch

from temporalio.client import WorkflowExecutionStatus, WorkflowFailureError
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import ApplicationError, WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode

from sefir_agent.providers import ProviderConfigurationError
from sefir_agent.web_bridge import dispatch


def load_bridge_functions() -> dict[str, Any]:
    source = Path(__file__).parents[1] / "src" / "sefir_agent"
    namespace = {
        "Any": Any,
        "Client": Any,
        "dataclass": dataclass,
        "WorkflowExecutionStatus": WorkflowExecutionStatus,
        "WorkflowFailureError": WorkflowFailureError,
        "WorkflowIDConflictPolicy": WorkflowIDConflictPolicy,
        "WorkflowIDReusePolicy": WorkflowIDReusePolicy,
        "WorkflowAlreadyStartedError": WorkflowAlreadyStartedError,
        "RPCError": RPCError,
        "RPCStatusCode": RPCStatusCode,
    }
    for filename, allowed in (
        ("workflow_types.py", {"ApprovalWorkflowInput", "DraftApproval"}),
        ("web_bridge.py", {"workflow_status", "approve", "stop_approval_workflow"}),
    ):
        tree = ast.parse((source / filename).read_text())
        tree.body = [
            node
            for node in tree.body
            if isinstance(node, (ast.ClassDef, ast.AsyncFunctionDef))
            and node.name in allowed
        ]
        exec(compile(tree, filename, "exec"), namespace)
    return namespace


class WebBridgeTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self) -> None:
        self.functions = load_bridge_functions()
        self.handle = MagicMock()
        self.handle.describe = AsyncMock(
            return_value=SimpleNamespace(
                run_id="specific-run", status=WorkflowExecutionStatus.RUNNING
            )
        )
        self.handle.signal = AsyncMock()
        self.handle.result = AsyncMock(return_value={"state": "sent"})
        self.client = MagicMock()
        self.client.get_workflow_handle.return_value = self.handle
        self.client.start_workflow = AsyncMock()
        self.payload = {
            "draft_id": "draft-test",
            "version": 1,
            "content_hash": "a" * 64,
            "approval_id": "approval-test",
            "approved_by": "Owner",
        }

    async def test_running_status_does_not_wait_for_result(self) -> None:
        result = await self.functions["workflow_status"](self.client, "draft-test", 1)
        self.assertEqual(result["status"], "RUNNING")
        self.handle.result.assert_not_awaited()

    async def test_missing_workflow_is_not_started(self) -> None:
        self.handle.describe.side_effect = RPCError(
            "missing", RPCStatusCode.NOT_FOUND, b""
        )
        result = await self.functions["workflow_status"](self.client, "draft-test", 1)
        self.assertEqual(result["status"], "NOT_STARTED")

    async def test_completed_result_is_bound_to_described_run(self) -> None:
        self.handle.describe.return_value.status = WorkflowExecutionStatus.COMPLETED
        result = await self.functions["workflow_status"](self.client, "draft-test", 1)
        self.assertEqual(result["delivery"], {"state": "sent"})
        self.assertEqual(
            self.client.get_workflow_handle.call_args.kwargs["run_id"], "specific-run"
        )
        self.handle.result.assert_awaited_once_with(follow_runs=False)

    async def test_failed_workflow_exposes_failure(self) -> None:
        self.handle.describe.return_value.status = WorkflowExecutionStatus.FAILED
        self.handle.result.side_effect = WorkflowFailureError(
            cause=ApplicationError("Delivery was not confirmed")
        )
        result = await self.functions["workflow_status"](self.client, "draft-test", 1)
        self.assertIn("Delivery was not confirmed", result["failure"])

    async def test_approval_starts_and_signals_exact_draft(self) -> None:
        await self.functions["approve"](self.client, self.payload)
        options = self.client.start_workflow.call_args.kwargs
        self.assertEqual(options["id"], "approval-draft-test-v1")
        self.assertEqual(
            options["id_reuse_policy"], WorkflowIDReusePolicy.REJECT_DUPLICATE
        )
        approval = self.handle.signal.call_args.args[1]
        self.assertEqual(approval.content_hash, self.payload["content_hash"])
        self.assertEqual(approval.approval_id, self.payload["approval_id"])

    async def test_retry_uses_existing_running_workflow(self) -> None:
        self.client.start_workflow.side_effect = WorkflowAlreadyStartedError(
            "approval-draft-test-v1", "ApprovalWorkflow"
        )
        await self.functions["approve"](self.client, self.payload)
        self.handle.signal.assert_awaited_once()

    async def test_closed_workflow_is_not_signaled_or_restarted(self) -> None:
        self.client.start_workflow.side_effect = WorkflowAlreadyStartedError(
            "approval-draft-test-v1", "ApprovalWorkflow"
        )
        self.handle.describe.return_value.status = WorkflowExecutionStatus.COMPLETED
        result = await self.functions["approve"](self.client, self.payload)
        self.assertEqual(result["status"], "COMPLETED")
        self.handle.signal.assert_not_awaited()

    async def test_stop_approval_workflow_targets_described_run(self) -> None:
        run_handle = MagicMock()
        run_handle.terminate = AsyncMock()
        self.handle.terminate = AsyncMock()
        self.client.get_workflow_handle.side_effect = [self.handle, run_handle]

        await self.functions["stop_approval_workflow"](
            self.client, "approval-draft-test-v1"
        )

        self.handle.describe.assert_awaited_once_with()
        self.assertEqual(
            self.client.get_workflow_handle.call_args_list,
            [
                call("approval-draft-test-v1"),
                call("approval-draft-test-v1", run_id="specific-run"),
            ],
        )
        run_handle.terminate.assert_awaited_once_with(
            reason="Account deletion requested"
        )
        self.handle.terminate.assert_not_awaited()

    async def test_stop_approval_workflow_skips_closed_workflows(self) -> None:
        self.handle.terminate = AsyncMock()
        for status in (
            WorkflowExecutionStatus.COMPLETED,
            WorkflowExecutionStatus.FAILED,
            WorkflowExecutionStatus.CANCELED,
            WorkflowExecutionStatus.TERMINATED,
            WorkflowExecutionStatus.TIMED_OUT,
        ):
            with self.subTest(status=status):
                self.handle.describe.return_value.status = status
                await self.functions["stop_approval_workflow"](
                    self.client, "approval-draft-test-v1"
                )
                self.handle.terminate.assert_not_awaited()

    async def test_stop_approval_workflow_ignores_missing_workflow(self) -> None:
        self.handle.terminate = AsyncMock()

        self.handle.describe.side_effect = RPCError(
            "missing", RPCStatusCode.NOT_FOUND, b""
        )
        await self.functions["stop_approval_workflow"](
            self.client, "approval-draft-test-v1"
        )

        self.handle.describe.assert_awaited_once_with()
        self.handle.terminate.assert_not_awaited()

    async def test_stop_approval_workflow_ignores_missing_run_on_termination(
        self,
    ) -> None:

        self.handle.describe = AsyncMock(
            return_value=SimpleNamespace(
                run_id="specific-run", status=WorkflowExecutionStatus.RUNNING
            )
        )

        self.handle.terminate = AsyncMock(
            side_effect=RPCError("missing", RPCStatusCode.NOT_FOUND, b"")
        )

        await self.functions["stop_approval_workflow"](
            self.client, "approval-draft-test-v1"
        )

        self.handle.terminate.assert_awaited_once_with(
            reason="Account deletion requested"
        )

        self.handle.describe.assert_awaited_once_with()

    async def test_stop_approval_workflow_rejects_unknown_status(self) -> None:
        self.handle.describe.return_value.status = None
        self.handle.terminate = AsyncMock()
        with self.assertRaisesRegex(RuntimeError, "Unknown workflow status"):
            await self.functions["stop_approval_workflow"](
                self.client, "approval-draft-test-v1"
            )
        self.handle.terminate.assert_not_awaited()

    async def test_stop_approval_workflow_propagates_rpc_errors(self) -> None:
        for operation in ("describe", "terminate"):
            with self.subTest(operation=operation):
                self.handle.describe.side_effect = None
                self.handle.terminate = AsyncMock()
                error = RPCError("unavailable", RPCStatusCode.UNAVAILABLE, b"")
                if operation == "describe":
                    self.handle.describe.side_effect = error
                else:
                    self.handle.terminate.side_effect = error
                with self.assertRaises(RPCError) as caught:
                    await self.functions["stop_approval_workflow"](
                        self.client, "approval-draft-test-v1"
                    )
                self.assertIs(caught.exception, error)


class ProviderBridgeTests(unittest.IsolatedAsyncioTestCase):
    async def test_catalog_does_not_contact_a_model_or_temporal(self) -> None:
        with (
            patch.dict(os.environ, {"OPENAI_API_KEY": "openai-test-key"}, clear=True),
            patch(
                "sefir_agent.web_bridge.Client.connect", new_callable=AsyncMock
            ) as connect,
        ):
            catalog = await dispatch("providers", {})
        self.assertEqual(catalog["default_provider"], "openai")
        self.assertNotIn("openai-test-key", str(catalog))
        connect.assert_not_awaited()

    async def test_generate_passes_provider_to_drafting(self) -> None:
        draft = {"subject": "Hello", "body": "A fictional introduction."}
        generate = AsyncMock(return_value=SimpleNamespace(model_dump=lambda: draft))
        with patch.dict(
            sys.modules,
            {"sefir_agent.drafting": SimpleNamespace(draft_outreach=generate)},
        ):
            result = await dispatch(
                "generate",
                {
                    "contact": {"name": "Alex Example"},
                    "cv_text": "Fictional resume",
                    "provider": "anthropic",
                },
            )
        self.assertEqual(result, draft)
        generate.assert_awaited_once_with(
            {"name": "Alex Example"}, "Fictional resume", "anthropic"
        )

    async def test_provider_configuration_errors_are_sanitized(self) -> None:
        generate = AsyncMock(side_effect=ProviderConfigurationError("private details"))
        with patch.dict(
            sys.modules,
            {"sefir_agent.drafting": SimpleNamespace(draft_outreach=generate)},
        ):
            result = await dispatch(
                "generate",
                {"contact": {}, "cv_text": "Fictional resume", "provider": "openai"},
            )
        self.assertEqual(result, {"error": "provider_unavailable"})
        generate.assert_awaited_once()
