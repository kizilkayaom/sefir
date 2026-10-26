import unittest
from unittest.mock import AsyncMock, patch

from temporalio.exceptions import ActivityError, ApplicationError, RetryState

from sefir_agent.workflow_types import ApprovalWorkflowInput, DraftApproval
from sefir_agent.workflows import ApprovalWorkflow


class ApprovalWorkflowTests(unittest.IsolatedAsyncioTestCase):
    async def test_approved_draft_is_sent(self) -> None:
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
        instance = ApprovalWorkflow(request)
        instance.approve(approval)

        approval_result = {"outbox_id": "outbox-test"}
        delivery_result = {
            "outbox_id": "outbox-test",
            "state": "sent",
            "gmail_message_id": "gmail-test",
            "last_error": None,
        }
        execute_activity = AsyncMock(side_effect=[approval_result, delivery_result])

        with (
            patch(
                "sefir_agent.workflows.workflow.wait_condition",
                new_callable=AsyncMock,
            ),
            patch(
                "sefir_agent.workflows.workflow.execute_activity",
                execute_activity,
            ),
        ):
            result = await instance.run(request)

        self.assertEqual(result, delivery_result)
        self.assertEqual(execute_activity.await_count, 2)
        self.assertEqual(
            execute_activity.await_args_list[0].args,
            ("record_draft_approval", approval),
        )
        self.assertEqual(
            execute_activity.await_args_list[1].args,
            ("deliver_outbox_message", "outbox-test"),
        )

    async def test_unconfirmed_delivery_raises_error(self) -> None:
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
        cases = (
            ("failed", "Gmail rejected the message"),
            ("needs_attention", "Send outcome is uncertain"),
            ("sending", None),
            ("pending", None),
        )

        for state, last_error in cases:
            with self.subTest(state=state):
                instance = ApprovalWorkflow(request)
                instance.approve(approval)

                approval_result = {"outbox_id": "outbox-test"}
                delivery_result = {
                    "outbox_id": "outbox-test",
                    "state": state,
                    "gmail_message_id": None,
                    "last_error": last_error,
                }
                execute_activity = AsyncMock(
                    side_effect=[approval_result, delivery_result]
                )

                with (
                    patch(
                        "sefir_agent.workflows.workflow.wait_condition",
                        new_callable=AsyncMock,
                    ),
                    patch(
                        "sefir_agent.workflows.workflow.execute_activity",
                        execute_activity,
                    ),
                    self.assertRaises(ApplicationError) as raised,
                ):
                    await instance.run(request)

                error = raised.exception
                self.assertEqual(error.type, "DeliveryNotConfirmed")
                self.assertTrue(error.non_retryable)
                self.assertEqual(error.details, (delivery_result,))
                self.assertIn("outbox-test", error.message)
                self.assertIn(state, error.message)
                if last_error is not None:
                    self.assertIn(last_error, error.message)
                self.assertEqual(execute_activity.await_count, 2)

    async def test_approval_activity_failure_prevents_delivery(self) -> None:
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
        instance = ApprovalWorkflow(request)
        instance.approve(approval)

        error = ActivityError(
            "Approval failed",
            scheduled_event_id=1,
            started_event_id=2,
            identity="test-worker",
            activity_type="record_draft_approval",
            activity_id="approval-test",
            retry_state=RetryState.MAXIMUM_ATTEMPTS_REACHED,
        )
        execute_activity = AsyncMock(side_effect=error)

        with (
            patch(
                "sefir_agent.workflows.workflow.wait_condition",
                new_callable=AsyncMock,
            ),
            patch(
                "sefir_agent.workflows.workflow.execute_activity",
                execute_activity,
            ),
            self.assertRaises(ActivityError) as raised,
        ):
            await instance.run(request)

        self.assertIs(raised.exception, error)
        self.assertEqual(execute_activity.await_count, 1)
        self.assertEqual(
            execute_activity.await_args_list[0].args,
            ("record_draft_approval", approval),
        )

    async def test_delivery_activity_failure_propagates(self) -> None:
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
        instance = ApprovalWorkflow(request)
        instance.approve(approval)

        approval_result = {"outbox_id": "outbox-test"}
        error = ActivityError(
            "Delivery failed",
            scheduled_event_id=3,
            started_event_id=4,
            identity="test-worker",
            activity_type="deliver_outbox_message",
            activity_id="delivery-test",
            retry_state=RetryState.MAXIMUM_ATTEMPTS_REACHED,
        )
        execute_activity = AsyncMock(side_effect=[approval_result, error])

        with (
            patch(
                "sefir_agent.workflows.workflow.wait_condition",
                new_callable=AsyncMock,
            ),
            patch(
                "sefir_agent.workflows.workflow.execute_activity",
                execute_activity,
            ),
            self.assertRaises(ActivityError) as raised,
        ):
            await instance.run(request)

        self.assertIs(raised.exception, error)
        self.assertEqual(execute_activity.await_count, 2)
        self.assertEqual(
            execute_activity.await_args_list[0].args,
            ("record_draft_approval", approval),
        )
        self.assertEqual(
            execute_activity.await_args_list[1].args,
            ("deliver_outbox_message", "outbox-test"),
        )
