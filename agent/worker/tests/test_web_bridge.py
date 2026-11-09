import ast
import os
import re
import sys
import unittest
from dataclasses import dataclass, replace
from datetime import timedelta
from pathlib import Path
from types import SimpleNamespace
from typing import Any
from unittest.mock import AsyncMock, MagicMock, call, patch

from temporalio.api.common.v1 import WorkflowExecution
from temporalio.api.history.v1 import (
    HistoryEvent,
    WorkflowExecutionStartedEventAttributes,
)
from temporalio.api.workflowservice.v1 import DeleteWorkflowExecutionRequest
from temporalio.client import WorkflowExecutionStatus, WorkflowFailureError
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import ApplicationError, WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode

from sefir_agent.providers import ProviderConfigurationError
from sefir_agent.styles import PRESETS
from sefir_agent.web_bridge import dispatch


def load_bridge_functions() -> dict[str, Any]:
    source = Path(__file__).parents[1] / "src" / "sefir_agent"
    namespace = {
        "Any": Any,
        "Client": Any,
        "dataclass": dataclass,
        "timedelta": timedelta,
        "re": re,
        "WorkflowExecution": WorkflowExecution,
        "DeleteWorkflowExecutionRequest": DeleteWorkflowExecutionRequest,
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
        (
            "web_bridge.py",
            {
                "workflow_status",
                "approve",
                "stop_approval_workflow",
                "request_approval_run_deletion",
                "approval_run_deleted",
                "validate_approval_workflow_id",
                "list_approval_runs",
            },
        ),
    ):
        tree = ast.parse((source / filename).read_text())
        tree.body = [
            node
            for node in tree.body
            if isinstance(node, (ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef))
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
        self.handle.fetch_history_events.return_value.__aiter__.return_value = [
            HistoryEvent(
                workflow_execution_started_event_attributes=WorkflowExecutionStartedEventAttributes()
            )
        ]
        self.handle.result = AsyncMock(return_value={"state": "sent"})
        self.client = MagicMock()
        self.client.get_workflow_handle.return_value = self.handle
        self.client.start_workflow = AsyncMock()
        self.client.namespace = "test-namespace"
        self.client.workflow_service.delete_workflow_execution = AsyncMock()
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
        self.assertEqual(options["rpc_timeout"], timedelta(seconds=10))
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

    async def test_stop_approval_dispatch_calls_helper(self) -> None:
        with (
            patch(
                "sefir_agent.web_bridge.Client.connect",
                new_callable=AsyncMock,
                return_value=self.client,
            ) as connect,
            patch(
                "sefir_agent.web_bridge.stop_approval_workflow", new_callable=AsyncMock
            ) as stop,
        ):
            result = await dispatch(
                "stop_approval", {"workflow_id": "approval-draft-test-v1"}
            )
        connect.assert_awaited_once()
        stop.assert_awaited_once_with(self.client, "approval-draft-test-v1")
        self.assertEqual(
            result,
            {"workflow_id": "approval-draft-test-v1", "stopped": True},
        )

    async def test_stop_approval_dispatch_rejects_invalid_workflow_ids(self) -> None:
        payloads = (
            {},
            {"workflow_id": None},
            {"workflow_id": ""},
            {"workflow_id": " \t\n"},
            {"workflow_id": 123},
            {"workflow_id": False},
            {"workflow_id": []},
            {"workflow_id": {}},
        )
        for payload in payloads:
            with (
                self.subTest(payload=payload),
                patch(
                    "sefir_agent.web_bridge.Client.connect",
                    new_callable=AsyncMock,
                    return_value=self.client,
                ) as connect,
                patch(
                    "sefir_agent.web_bridge.stop_approval_workflow",
                    new_callable=AsyncMock,
                ) as stop,
            ):
                with self.assertRaisesRegex(
                    ValueError, "workflow_id must be a non-empty string"
                ):
                    await dispatch("stop_approval", payload)

                connect.assert_not_awaited()
                stop.assert_not_awaited()

    async def test_stop_approval_dispatch_propagates_helper_errors(self) -> None:
        error = RPCError("unavailable", RPCStatusCode.UNAVAILABLE, b"")
        with (
            patch(
                "sefir_agent.web_bridge.Client.connect",
                new_callable=AsyncMock,
                return_value=self.client,
            ) as connect,
            patch(
                "sefir_agent.web_bridge.stop_approval_workflow",
                new_callable=AsyncMock,
                side_effect=error,
            ) as stop,
            self.assertRaises(RPCError) as caught,
        ):
            await dispatch("stop_approval", {"workflow_id": "approval-draft-test-v1"})

        self.assertIs(caught.exception, error)
        connect.assert_awaited_once()
        stop.assert_awaited_once_with(self.client, "approval-draft-test-v1")

    async def test_run_deletion_dispatch_calls_matching_helper(self) -> None:
        payload = {"workflow_id": "approval-draft-test-v1", "run_id": "specific-run"}
        for action, helper_result, response in (
            ("request_approval_run_deletion", None, {"deletion_requested": True}),
            ("approval_run_deleted", False, {"deleted": False}),
            ("approval_run_deleted", True, {"deleted": True}),
        ):
            with (
                self.subTest(action=action, helper_result=helper_result),
                patch(
                    "sefir_agent.web_bridge.Client.connect",
                    new_callable=AsyncMock,
                    return_value=self.client,
                ) as connect,
                patch(
                    f"sefir_agent.web_bridge.{action}",
                    new_callable=AsyncMock,
                    return_value=helper_result,
                ) as helper,
            ):
                result = await dispatch(action, payload)

                self.assertEqual(result, {**payload, **response})
                connect.assert_awaited_once()
                helper.assert_awaited_once_with(
                    self.client, payload["workflow_id"], payload["run_id"]
                )
                self.client.get_workflow_handle.assert_not_called()

    async def test_run_deletion_dispatch_rejects_invalid_ids_before_connecting(
        self,
    ) -> None:
        valid_payload = {
            "workflow_id": "approval-draft-test-v1",
            "run_id": "specific-run",
        }
        for action in ("request_approval_run_deletion", "approval_run_deleted"):
            for field in ("workflow_id", "run_id"):
                payloads = [
                    {**valid_payload, field: value}
                    for value in (None, "", " \t\n", 123, False, [], {})
                ]
                missing_payload = valid_payload.copy()
                del missing_payload[field]
                payloads.append(missing_payload)
                for payload in payloads:
                    with (
                        self.subTest(action=action, field=field, payload=payload),
                        patch(
                            "sefir_agent.web_bridge.Client.connect",
                            new_callable=AsyncMock,
                            return_value=self.client,
                        ) as connect,
                        patch(
                            f"sefir_agent.web_bridge.{action}",
                            new_callable=AsyncMock,
                        ) as helper,
                    ):
                        with self.assertRaisesRegex(
                            ValueError, f"{field} must be a non-empty string"
                        ):
                            await dispatch(action, payload)

                        connect.assert_not_awaited()
                        helper.assert_not_awaited()

    async def test_run_deletion_dispatch_propagates_helper_errors(self) -> None:
        payload = {"workflow_id": "approval-draft-test-v1", "run_id": "specific-run"}
        for action in ("request_approval_run_deletion", "approval_run_deleted"):
            for error in (
                ValueError("invalid run"),
                RuntimeError("run is still running"),
                RPCError("unavailable", RPCStatusCode.UNAVAILABLE, b""),
            ):
                with (
                    self.subTest(action=action, error=type(error).__name__),
                    patch(
                        "sefir_agent.web_bridge.Client.connect",
                        new_callable=AsyncMock,
                        return_value=self.client,
                    ) as connect,
                    patch(
                        f"sefir_agent.web_bridge.{action}",
                        new_callable=AsyncMock,
                        side_effect=error,
                    ) as helper,
                    self.assertRaises(type(error)) as caught,
                ):
                    await dispatch(action, payload)

                self.assertIs(caught.exception, error)
                connect.assert_awaited_once()
                helper.assert_awaited_once_with(
                    self.client, payload["workflow_id"], payload["run_id"]
                )

    async def test_run_deletion_dispatch_propagates_connection_errors(self) -> None:
        payload = {"workflow_id": "approval-draft-test-v1", "run_id": "specific-run"}
        for action in ("request_approval_run_deletion", "approval_run_deleted"):
            error = RuntimeError("connection failed")
            with (
                self.subTest(action=action),
                patch(
                    "sefir_agent.web_bridge.Client.connect",
                    new_callable=AsyncMock,
                    side_effect=error,
                ) as connect,
                patch(
                    f"sefir_agent.web_bridge.{action}", new_callable=AsyncMock
                ) as helper,
                self.assertRaises(RuntimeError) as caught,
            ):
                await dispatch(action, payload)

            self.assertIs(caught.exception, error)
            connect.assert_awaited_once()
            helper.assert_not_awaited()

    async def test_request_approval_run_deletion_targets_exact_run(self) -> None:
        workflow_id = "approval-draft-test-v1"
        run_id = "specific-run"
        self.handle.describe.return_value.run_id = "different-run"
        delete = self.client.workflow_service.delete_workflow_execution
        for status in (
            WorkflowExecutionStatus.CONTINUED_AS_NEW,
            WorkflowExecutionStatus.COMPLETED,
            WorkflowExecutionStatus.FAILED,
            WorkflowExecutionStatus.CANCELED,
            WorkflowExecutionStatus.TERMINATED,
            WorkflowExecutionStatus.TIMED_OUT,
        ):
            with self.subTest(status=status):
                self.client.get_workflow_handle.reset_mock()
                self.handle.describe.reset_mock()
                delete.reset_mock()
                self.handle.describe.return_value.status = status

                await self.functions["request_approval_run_deletion"](
                    self.client, workflow_id, run_id
                )

                self.client.get_workflow_handle.assert_called_once_with(
                    workflow_id, run_id=run_id
                )
                self.handle.describe.assert_awaited_once_with()
                delete.assert_awaited_once_with(
                    DeleteWorkflowExecutionRequest(
                        namespace="test-namespace",
                        workflow_execution=WorkflowExecution(
                            workflow_id=workflow_id, run_id=run_id
                        ),
                    )
                )

    async def test_request_approval_run_deletion_rejects_invalid_ids(self) -> None:
        for field in ("workflow_id", "run_id"):
            for value in (None, "", " \t\n", 123, False, [], {}):
                with self.subTest(field=field, value=value):
                    arguments = {
                        "workflow_id": "approval-draft-test-v1",
                        "run_id": "specific-run",
                    }
                    arguments[field] = value
                    with self.assertRaisesRegex(
                        ValueError, f"{field} must be a non-empty string"
                    ):
                        await self.functions["request_approval_run_deletion"](
                            self.client, **arguments
                        )

                    self.client.get_workflow_handle.assert_not_called()
                    self.handle.describe.assert_not_awaited()
                    self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_request_approval_run_deletion_rejects_unsupported_statuses(
        self,
    ) -> None:
        for status in (
            WorkflowExecutionStatus.RUNNING,
            None,
        ):
            with self.subTest(status=status):
                self.handle.describe.return_value.status = status
                with self.assertRaisesRegex(
                    RuntimeError, "Workflow run must be closed before deletion"
                ):
                    await self.functions["request_approval_run_deletion"](
                        self.client, "approval-draft-test-v1", "specific-run"
                    )
                self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_request_approval_run_deletion_ignores_missing_run(self) -> None:
        self.handle.describe.side_effect = RPCError(
            "missing", RPCStatusCode.NOT_FOUND, b""
        )

        await self.functions["request_approval_run_deletion"](
            self.client, "approval-draft-test-v1", "specific-run"
        )

        self.handle.describe.assert_awaited_once_with()
        self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_request_approval_run_deletion_ignores_missing_run_on_deletion(
        self,
    ) -> None:
        self.handle.describe.return_value.status = WorkflowExecutionStatus.TERMINATED
        delete = self.client.workflow_service.delete_workflow_execution
        delete.side_effect = RPCError("missing", RPCStatusCode.NOT_FOUND, b"")

        await self.functions["request_approval_run_deletion"](
            self.client, "approval-draft-test-v1", "specific-run"
        )

        self.handle.describe.assert_awaited_once_with()
        delete.assert_awaited_once()

    async def test_request_approval_run_deletion_propagates_rpc_errors(self) -> None:
        self.handle.describe.return_value.status = WorkflowExecutionStatus.TERMINATED
        delete = self.client.workflow_service.delete_workflow_execution
        for operation in ("describe", "delete"):
            with self.subTest(operation=operation):
                self.handle.describe.reset_mock(side_effect=True)
                delete.reset_mock(side_effect=True)
                error = RPCError("unavailable", RPCStatusCode.UNAVAILABLE, b"")
                if operation == "describe":
                    self.handle.describe.side_effect = error
                else:
                    delete.side_effect = error

                with self.assertRaises(RPCError) as caught:
                    await self.functions["request_approval_run_deletion"](
                        self.client, "approval-draft-test-v1", "specific-run"
                    )

                self.assertIs(caught.exception, error)
                self.handle.describe.assert_awaited_once_with()
                if operation == "describe":
                    delete.assert_not_awaited()
                else:
                    delete.assert_awaited_once()

    async def test_approval_run_deleted_reports_existing_run(self) -> None:
        result = await self.functions["approval_run_deleted"](
            self.client, "approval-draft-test-v1", "specific-run"
        )

        self.assertIs(result, False)
        self.client.get_workflow_handle.assert_called_once_with(
            "approval-draft-test-v1", run_id="specific-run"
        )
        self.handle.describe.assert_awaited_once_with()
        self.handle.fetch_history_events.assert_not_called()
        self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_approval_run_deleted_requires_missing_execution_and_history(
        self,
    ) -> None:
        self.handle.describe.side_effect = RPCError(
            "missing", RPCStatusCode.NOT_FOUND, b""
        )
        events = MagicMock()
        events.__anext__ = AsyncMock(
            side_effect=RPCError("missing history", RPCStatusCode.NOT_FOUND, b"")
        )
        self.handle.fetch_history_events.return_value = events

        result = await self.functions["approval_run_deleted"](
            self.client, "approval-draft-test-v1", "specific-run"
        )

        self.assertIs(result, True)
        self.client.get_workflow_handle.assert_called_once_with(
            "approval-draft-test-v1", run_id="specific-run"
        )
        self.handle.describe.assert_awaited_once_with()
        self.handle.fetch_history_events.assert_called_once_with(page_size=1)
        events.__anext__.assert_awaited_once_with()
        self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_approval_run_deleted_retains_accessible_history(self) -> None:
        self.handle.describe.side_effect = RPCError(
            "missing", RPCStatusCode.NOT_FOUND, b""
        )
        for name, outcome in (
            ("event exists", SimpleNamespace(event_id=1)),
            ("empty history", StopAsyncIteration()),
        ):
            with self.subTest(name=name):
                self.handle.describe.reset_mock()
                self.handle.fetch_history_events.reset_mock()
                events = MagicMock()
                events.__anext__ = AsyncMock(side_effect=[outcome])
                self.handle.fetch_history_events.return_value = events

                result = await self.functions["approval_run_deleted"](
                    self.client, "approval-draft-test-v1", "specific-run"
                )

                self.assertIs(result, False)
                self.handle.describe.assert_awaited_once_with()
                self.handle.fetch_history_events.assert_called_once_with(page_size=1)
                events.__anext__.assert_awaited_once_with()
                self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_approval_run_deleted_propagates_rpc_errors(self) -> None:
        for operation in ("describe", "history"):
            for status in (RPCStatusCode.UNAVAILABLE, RPCStatusCode.PERMISSION_DENIED):
                with self.subTest(operation=operation, status=status):
                    self.handle.describe.reset_mock(side_effect=True)
                    self.handle.fetch_history_events.reset_mock()
                    error = RPCError("lookup failed", status, b"")
                    events = MagicMock()
                    events.__anext__ = AsyncMock(side_effect=error)
                    self.handle.fetch_history_events.return_value = events
                    if operation == "describe":
                        self.handle.describe.side_effect = error
                    else:
                        self.handle.describe.side_effect = RPCError(
                            "missing", RPCStatusCode.NOT_FOUND, b""
                        )

                    with self.assertRaises(RPCError) as caught:
                        await self.functions["approval_run_deleted"](
                            self.client, "approval-draft-test-v1", "specific-run"
                        )

                    self.assertIs(caught.exception, error)
                    self.handle.describe.assert_awaited_once_with()
                    if operation == "describe":
                        self.handle.fetch_history_events.assert_not_called()
                        events.__anext__.assert_not_awaited()
                    else:
                        self.handle.fetch_history_events.assert_called_once_with(
                            page_size=1
                        )
                        events.__anext__.assert_awaited_once_with()
                    self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_approval_run_deleted_rejects_invalid_ids(self) -> None:
        for field in ("workflow_id", "run_id"):
            for value in (None, "", " \t\n", 123, False, [], {}):
                with self.subTest(field=field, value=value):
                    arguments = {
                        "workflow_id": "approval-draft-test-v1",
                        "run_id": "specific-run",
                    }
                    arguments[field] = value
                    with self.assertRaisesRegex(
                        ValueError, f"{field} must be a non-empty string"
                    ):
                        await self.functions["approval_run_deleted"](
                            self.client, **arguments
                        )

                    self.client.get_workflow_handle.assert_not_called()
                    self.handle.describe.assert_not_awaited()
                    self.handle.fetch_history_events.assert_not_called()
                    self.client.workflow_service.delete_workflow_execution.assert_not_awaited()

    async def test_list_approval_runs_dispatch_returns_helper_runs(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for run_ids in ([], ["run-one", "run-two"]):
            with (
                self.subTest(run_ids=run_ids),
                patch(
                    "sefir_agent.web_bridge.Client.connect",
                    new_callable=AsyncMock,
                    return_value=self.client,
                ) as connect,
                patch(
                    "sefir_agent.web_bridge.list_approval_runs",
                    new_callable=AsyncMock,
                    return_value=run_ids,
                ) as helper,
            ):
                result = await dispatch(
                    "list_approval_runs", {"workflow_id": workflow_id}
                )

                self.assertEqual(
                    result, {"workflow_id": workflow_id, "run_ids": run_ids}
                )
                connect.assert_awaited_once()
                helper.assert_awaited_once_with(self.client, workflow_id)
                self.client.get_workflow_handle.assert_not_called()

    async def test_list_approval_runs_dispatch_rejects_invalid_ids_before_connecting(
        self,
    ) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        payloads = [{}] + [
            {"workflow_id": value}
            for value in (
                None,
                "",
                " \t\n",
                123,
                False,
                [],
                {},
                "approval-draft-test-v1",
                workflow_id.replace("approval-", "other-"),
                workflow_id.replace("11111111", "gggggggg", 1),
                workflow_id + "\n",
                " " + workflow_id,
                workflow_id + "' OR ExecutionStatus = 'Running",
                workflow_id[:-1] + "0",
                workflow_id[:-1] + "-1",
                workflow_id[:-1] + "01",
                workflow_id[:-1] + "1.5",
                workflow_id[:-1] + "\u0661",
            )
        ]
        for payload in payloads:
            with (
                self.subTest(payload=payload),
                patch(
                    "sefir_agent.web_bridge.Client.connect", new_callable=AsyncMock
                ) as connect,
                patch(
                    "sefir_agent.web_bridge.list_approval_runs",
                    new_callable=AsyncMock,
                ) as helper,
            ):
                with self.assertRaisesRegex(ValueError, "workflow_id must"):
                    await dispatch("list_approval_runs", payload)

                connect.assert_not_awaited()
                helper.assert_not_awaited()

    async def test_list_approval_runs_dispatch_propagates_helper_errors(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for error in (
            ValueError("invalid workflow"),
            RuntimeError("unexpected workflow"),
            RPCError("unavailable", RPCStatusCode.UNAVAILABLE, b""),
        ):
            with (
                self.subTest(error=type(error).__name__),
                patch(
                    "sefir_agent.web_bridge.Client.connect",
                    new_callable=AsyncMock,
                    return_value=self.client,
                ) as connect,
                patch(
                    "sefir_agent.web_bridge.list_approval_runs",
                    new_callable=AsyncMock,
                    side_effect=error,
                ) as helper,
                self.assertRaises(type(error)) as caught,
            ):
                await dispatch("list_approval_runs", {"workflow_id": workflow_id})

            self.assertIs(caught.exception, error)
            connect.assert_awaited_once()
            helper.assert_awaited_once_with(self.client, workflow_id)

    async def test_list_approval_runs_dispatch_propagates_connection_errors(
        self,
    ) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for error in (
            RuntimeError("connection failed"),
            RPCError("unavailable", RPCStatusCode.UNAVAILABLE, b""),
        ):
            with (
                self.subTest(error=type(error).__name__),
                patch(
                    "sefir_agent.web_bridge.Client.connect",
                    new_callable=AsyncMock,
                    side_effect=error,
                ) as connect,
                patch(
                    "sefir_agent.web_bridge.list_approval_runs",
                    new_callable=AsyncMock,
                ) as helper,
                self.assertRaises(type(error)) as caught,
            ):
                await dispatch("list_approval_runs", {"workflow_id": workflow_id})

            self.assertIs(caught.exception, error)
            connect.assert_awaited_once()
            helper.assert_not_awaited()

    async def test_list_approval_runs_merges_and_sorts_unique_runs(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        self.client.list_workflows.return_value.__aiter__.return_value = [
            SimpleNamespace(id=workflow_id, run_id=run_id)
            for run_id in ("run-two", "specific-run", "run-one", "run-two")
        ]

        result = await self.functions["list_approval_runs"](self.client, workflow_id)

        self.assertEqual(result, ["run-one", "run-two", "specific-run"])
        self.client.get_workflow_handle.assert_any_call(workflow_id)
        self.handle.describe.assert_awaited_once_with()
        self.client.list_workflows.assert_called_once_with(
            query=f"WorkflowId = '{workflow_id}'"
        )

    async def test_list_approval_runs_retains_run_missing_from_listing(self) -> None:
        workflow_id = "approval-aabbccdd-1111-4111-8111-111111111111-v12"
        self.client.list_workflows.return_value.__aiter__.return_value = []

        result = await self.functions["list_approval_runs"](self.client, workflow_id)

        self.assertEqual(result, ["specific-run"])
        self.client.list_workflows.assert_called_once_with(
            query=f"WorkflowId = '{workflow_id}'"
        )

    async def test_list_approval_runs_follows_history_links_without_visibility(
        self,
    ) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        self.handle.describe.return_value.run_id = "latest"
        self.client.list_workflows.return_value.__aiter__.return_value = []
        handles = {}
        for run_id, previous, original in (
            ("latest", "previous", "reset-source"),
            ("previous", "first", "previous"),
            ("reset-source", "first", "reset-source"),
            ("first", "", "first"),
        ):
            handle = MagicMock()
            handle.fetch_history_events.return_value.__aiter__.return_value = [
                HistoryEvent(
                    workflow_execution_started_event_attributes=WorkflowExecutionStartedEventAttributes(
                        continued_execution_run_id=previous,
                        first_execution_run_id="first",
                        original_execution_run_id=original,
                    )
                )
            ]
            handles[run_id] = handle
        self.client.get_workflow_handle.side_effect = lambda _, run_id=None: (
            handles[run_id] if run_id else self.handle
        )

        result = await self.functions["list_approval_runs"](self.client, workflow_id)

        self.assertEqual(result, ["first", "latest", "previous", "reset-source"])
        for handle in handles.values():
            handle.fetch_history_events.assert_called_once_with(page_size=1)

    async def test_list_approval_runs_requires_readable_start_events(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for events in ([], [HistoryEvent()]):
            with self.subTest(events=events):
                self.handle.fetch_history_events.return_value.__aiter__.return_value = (
                    events
                )
                with self.assertRaisesRegex(RuntimeError, "missing its start event"):
                    await self.functions["list_approval_runs"](self.client, workflow_id)
        for status in (RPCStatusCode.NOT_FOUND, RPCStatusCode.UNAVAILABLE):
            with self.subTest(status=status):
                self.handle.fetch_history_events.side_effect = RPCError(
                    "history error", status, b""
                )
                if status == RPCStatusCode.NOT_FOUND:
                    result = await self.functions["list_approval_runs"](
                        self.client, workflow_id
                    )
                    self.assertEqual(result, ["specific-run"])
                else:
                    with self.assertRaises(RPCError):
                        await self.functions["list_approval_runs"](
                            self.client, workflow_id
                        )

    async def test_list_approval_runs_lists_after_missing_description(self) -> None:
        workflow_id = "approval-AABBCCDD-1111-4111-8111-111111111111-v2"
        self.handle.describe.side_effect = RPCError(
            "missing", RPCStatusCode.NOT_FOUND, b""
        )
        for run_ids in ([], ["older-run"]):
            with self.subTest(run_ids=run_ids):
                self.client.list_workflows.reset_mock()
                self.client.list_workflows.return_value.__aiter__.return_value = [
                    SimpleNamespace(id=workflow_id, run_id=run_id) for run_id in run_ids
                ]

                result = await self.functions["list_approval_runs"](
                    self.client, workflow_id
                )

                self.assertEqual(result, run_ids)
                self.client.list_workflows.assert_called_once_with(
                    query=f"WorkflowId = '{workflow_id}'"
                )

    async def test_list_approval_runs_rejects_invalid_workflow_ids(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for value in (
            None,
            "",
            " \t\n",
            123,
            False,
            [],
            {},
            "approval-draft-test-v1",
            workflow_id.replace("approval-", "other-"),
            workflow_id.replace("11111111", "gggggggg", 1),
            workflow_id + "\n",
            " " + workflow_id,
            workflow_id + "' OR ExecutionStatus = 'Running",
            workflow_id[:-1] + "0",
            workflow_id[:-1] + "-1",
            workflow_id[:-1] + "01",
            workflow_id[:-1] + "1.5",
            workflow_id[:-1] + "\u0661",
        ):
            with self.subTest(value=value):
                with self.assertRaisesRegex(ValueError, "workflow_id must match"):
                    await self.functions["list_approval_runs"](self.client, value)

                self.client.get_workflow_handle.assert_not_called()
                self.client.list_workflows.assert_not_called()

    async def test_list_approval_runs_rejects_invalid_described_run(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for run_id in (None, "", " \t\n", 123, False, [], {}):
            with self.subTest(run_id=run_id):
                self.handle.describe.return_value.run_id = run_id

                with self.assertRaisesRegex(RuntimeError, "invalid workflow run ID"):
                    await self.functions["list_approval_runs"](self.client, workflow_id)

                self.client.list_workflows.assert_not_called()

    async def test_list_approval_runs_rejects_invalid_listed_runs(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for execution, message in (
            (
                SimpleNamespace(id=workflow_id[:-1] + "2", run_id="other-run"),
                "unexpected workflow ID",
            ),
            *(
                (
                    SimpleNamespace(id=workflow_id, run_id=run_id),
                    "invalid workflow run ID",
                )
                for run_id in (None, "", " \t\n", 123, False, [], {})
            ),
        ):
            with self.subTest(execution=execution):
                self.client.list_workflows.return_value.__aiter__.return_value = [
                    execution
                ]

                with self.assertRaisesRegex(RuntimeError, message):
                    await self.functions["list_approval_runs"](self.client, workflow_id)

    async def test_list_approval_runs_propagates_describe_errors(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for status in (RPCStatusCode.UNAVAILABLE, RPCStatusCode.PERMISSION_DENIED):
            with self.subTest(status=status):
                error = RPCError("describe failed", status, b"")
                self.handle.describe.side_effect = error

                with self.assertRaises(RPCError) as caught:
                    await self.functions["list_approval_runs"](self.client, workflow_id)

                self.assertIs(caught.exception, error)
                self.client.list_workflows.assert_not_called()

    async def test_list_approval_runs_propagates_listing_errors(self) -> None:
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        for status in (
            RPCStatusCode.NOT_FOUND,
            RPCStatusCode.UNAVAILABLE,
            RPCStatusCode.PERMISSION_DENIED,
        ):
            for partial in (False, True):
                with self.subTest(status=status, partial=partial):
                    error = RPCError("listing failed", status, b"")

                    async def executions(partial: bool, error: RPCError):
                        if partial:
                            yield SimpleNamespace(id=workflow_id, run_id="older-run")
                        raise error

                    self.client.list_workflows.return_value = executions(partial, error)

                    with self.assertRaises(RPCError) as caught:
                        await self.functions["list_approval_runs"](
                            self.client, workflow_id
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
        self.assertEqual(result, {**draft, "preset_id": "prof", "preset_revision": 1})
        generate.assert_awaited_once_with(
            {"name": "Alex Example"},
            "Fictional resume",
            "anthropic",
            preset_id="prof",
        )

    async def test_generate_passes_preset_to_drafting(self) -> None:
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
                    "preset_id": "formal",
                },
            )
        self.assertEqual(result, {**draft, "preset_id": "formal", "preset_revision": 1})
        generate.assert_awaited_once_with(
            {"name": "Alex Example"},
            "Fictional resume",
            "anthropic",
            preset_id="formal",
        )

    async def test_generate_returns_selected_preset_revision(self) -> None:
        draft = {"subject": "Hello", "body": "A fictional introduction."}
        generate = AsyncMock(return_value=SimpleNamespace(model_dump=lambda: draft))
        preset = replace(PRESETS["formal"], revision=2)
        with (
            patch.dict(PRESETS, {"formal": preset}),
            patch.dict(
                sys.modules,
                {"sefir_agent.drafting": SimpleNamespace(draft_outreach=generate)},
            ),
        ):
            result = await dispatch(
                "generate",
                {
                    "contact": {"name": "Alex Example"},
                    "cv_text": "Fictional resume",
                    "provider": "anthropic",
                    "preset_id": "formal",
                },
            )
        self.assertEqual(result, {**draft, "preset_id": "formal", "preset_revision": 2})
        generate.assert_awaited_once_with(
            {"name": "Alex Example"},
            "Fictional resume",
            "anthropic",
            preset_id="formal",
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
