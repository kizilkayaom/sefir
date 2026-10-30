import ast
import asyncio
import shutil
import unittest
from pathlib import Path
from typing import Any

from temporalio.client import Client, WorkflowExecutionStatus, WorkflowFailureError
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode
from temporalio.testing import WorkflowEnvironment
from temporalio.worker import Worker
from test_workflow_retries import FakeEmailActivities

from sefir_agent.workflow_types import ApprovalWorkflowInput, DraftApproval
from sefir_agent.workflows import ApprovalWorkflow


def bridge_functions() -> dict[str, Any]:
    source = Path(__file__).parents[1] / "src" / "sefir_agent" / "web_bridge.py"
    tree = ast.parse(source.read_text())
    tree.body = [
        node
        for node in tree.body
        if isinstance(node, ast.AsyncFunctionDef)
        and node.name in {"approve", "workflow_status"}
    ]
    namespace = {
        "Client": Client,
        "Any": Any,
        "WorkflowExecutionStatus": WorkflowExecutionStatus,
        "WorkflowFailureError": WorkflowFailureError,
        "WorkflowIDConflictPolicy": WorkflowIDConflictPolicy,
        "WorkflowIDReusePolicy": WorkflowIDReusePolicy,
        "WorkflowAlreadyStartedError": WorkflowAlreadyStartedError,
        "RPCError": RPCError,
        "RPCStatusCode": RPCStatusCode,
        "ApprovalWorkflowInput": ApprovalWorkflowInput,
        "DraftApproval": DraftApproval,
    }
    exec(compile(tree, str(source), "exec"), namespace)
    return namespace


class WebApprovalIntegrationTests(unittest.IsolatedAsyncioTestCase):
    async def test_repeated_web_approval_delivers_only_once(self) -> None:
        temporal_path = shutil.which("temporal")
        self.assertIsNotNone(temporal_path, "Temporal CLI is required")
        functions = bridge_functions()
        fake = FakeEmailActivities(approval_failures=0)
        payload = {
            "draft_id": "11111111-1111-4111-8111-111111111111",
            "version": 1,
            "content_hash": "a" * 64,
            "approval_id": "22222222-2222-4222-8222-222222222222",
            "approved_by": "Owner",
        }
        async with (
            await WorkflowEnvironment.start_local(
                dev_server_existing_path=temporal_path,
                dev_server_extra_args=["--disable-config-env", "--disable-config-file"],
            ) as environment,
            Worker(
                environment.client,
                task_queue="sefir-outreach",
                workflows=[ApprovalWorkflow],
                activities=[fake.record_draft_approval, fake.deliver_outbox_message],
            ),
        ):
            before = await functions["workflow_status"](
                environment.client, payload["draft_id"], payload["version"]
            )
            self.assertEqual(before["status"], "NOT_STARTED")
            await functions["approve"](environment.client, payload)
            await functions["approve"](environment.client, payload)
            handle = environment.client.get_workflow_handle(
                f"approval-{payload['draft_id']}-v1", result_type=dict[str, Any]
            )
            result = await asyncio.wait_for(handle.result(), timeout=30)
            repeated = await functions["approve"](environment.client, payload)

        self.assertEqual(result["state"], "sent")
        self.assertEqual(repeated["status"], "COMPLETED")
        self.assertEqual(repeated["delivery"], result)
        self.assertEqual(len(fake.received_approvals), 1)
        self.assertEqual(fake.delivered_outbox_ids, ["outbox-test"])
