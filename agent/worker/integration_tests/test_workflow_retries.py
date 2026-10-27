import asyncio
import shutil
import unittest
from typing import Any

from temporalio import activity
from temporalio.client import WorkflowFailureError
from temporalio.exceptions import ActivityError, RetryState
from temporalio.testing import WorkflowEnvironment
from temporalio.worker import Worker

from sefir_agent.workflow_types import ApprovalWorkflowInput, DraftApproval
from sefir_agent.workflows import ApprovalWorkflow


class FakeEmailActivities:
    def __init__(self, approval_failures: int = 1) -> None:
        self.approval_failures = approval_failures
        self.approval_attempts: list[int] = []
        self.received_approvals: list[DraftApproval] = []
        self.delivered_outbox_ids: list[str] = []

    @activity.defn(name="record_draft_approval")
    async def record_draft_approval(self, approval: DraftApproval) -> dict[str, Any]:
        attempt = activity.info().attempt
        self.approval_attempts.append(attempt)
        self.received_approvals.append(approval)
        if attempt <= self.approval_failures:
            raise RuntimeError("Simulated temporary failure")
        return {"outbox_id": "outbox-test"}

    @activity.defn(name="deliver_outbox_message")
    async def deliver_outbox_message(self, outbox_id: str) -> dict[str, Any]:
        self.delivered_outbox_ids.append(outbox_id)
        return {
            "outbox_id": outbox_id,
            "state": "sent",
            "gmail_message_id": "gmail-test",
            "last_error": None,
        }


class WorkflowRetryTests(unittest.IsolatedAsyncioTestCase):
    async def test_temporary_approval_failure_is_retried(self) -> None:
        temporal_path = shutil.which("temporal")
        self.assertIsNotNone(
            temporal_path,
            "Temporal CLI must be installed to run integration tests",
        )
        request = ApprovalWorkflowInput(
            draft_id="11111111-1111-4111-8111-111111111111",
            version=1,
            content_hash="a" * 64,
        )
        approval = DraftApproval(
            approval_id="22222222-2222-4222-8222-222222222222",
            draft_id=request.draft_id,
            version=request.version,
            content_hash=request.content_hash,
            approved_by="Test reviewer",
        )
        fake_activities = FakeEmailActivities()
        task_queue = "workflow-retry-test"

        async with (
            await WorkflowEnvironment.start_local(
                dev_server_existing_path=temporal_path,
                dev_server_extra_args=["--disable-config-env", "--disable-config-file"],
            ) as environment,
            Worker(
                environment.client,
                task_queue=task_queue,
                workflows=[ApprovalWorkflow],
                activities=[
                    fake_activities.record_draft_approval,
                    fake_activities.deliver_outbox_message,
                ],
            ),
        ):
            handle = await environment.client.start_workflow(
                ApprovalWorkflow.run,
                request,
                id="approval-retry-test",
                task_queue=task_queue,
            )
            await handle.signal(ApprovalWorkflow.approve, approval)
            result = await asyncio.wait_for(handle.result(), timeout=30)

        self.assertEqual(fake_activities.approval_attempts, [1, 2])
        self.assertEqual(fake_activities.received_approvals, [approval, approval])
        self.assertEqual(fake_activities.delivered_outbox_ids, ["outbox-test"])
        self.assertEqual(
            result,
            {
                "outbox_id": "outbox-test",
                "state": "sent",
                "gmail_message_id": "gmail-test",
                "last_error": None,
            },
        )

    async def test_approval_stops_after_maximum_attempts(self) -> None:
        temporal_path = shutil.which("temporal")
        self.assertIsNotNone(
            temporal_path,
            "Temporal CLI must be installed to run integration tests",
        )
        request = ApprovalWorkflowInput(
            draft_id="11111111-1111-4111-8111-111111111111",
            version=1,
            content_hash="a" * 64,
        )
        approval = DraftApproval(
            approval_id="22222222-2222-4222-8222-222222222222",
            draft_id=request.draft_id,
            version=request.version,
            content_hash=request.content_hash,
            approved_by="Test reviewer",
        )
        fake_activities = FakeEmailActivities(approval_failures=3)
        task_queue = "workflow-retry-limit-test"

        async with (
            await WorkflowEnvironment.start_local(
                dev_server_existing_path=temporal_path,
                dev_server_extra_args=["--disable-config-env", "--disable-config-file"],
            ) as environment,
            Worker(
                environment.client,
                task_queue=task_queue,
                workflows=[ApprovalWorkflow],
                activities=[
                    fake_activities.record_draft_approval,
                    fake_activities.deliver_outbox_message,
                ],
            ),
        ):
            handle = await environment.client.start_workflow(
                ApprovalWorkflow.run,
                request,
                id="approval-retry-limit-test",
                task_queue=task_queue,
            )
            await handle.signal(ApprovalWorkflow.approve, approval)
            with self.assertRaises(WorkflowFailureError) as raised:
                await asyncio.wait_for(handle.result(), timeout=30)

        self.assertEqual(fake_activities.approval_attempts, [1, 2, 3])
        self.assertEqual(
            fake_activities.received_approvals, [approval, approval, approval]
        )
        self.assertEqual(fake_activities.delivered_outbox_ids, [])
        activity_error = raised.exception.cause
        self.assertIsInstance(activity_error, ActivityError)
        self.assertEqual(
            activity_error.retry_state, RetryState.MAXIMUM_ATTEMPTS_REACHED
        )
