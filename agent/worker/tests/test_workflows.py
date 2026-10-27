import unittest
from dataclasses import replace
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

    async def test_invalid_approval_does_not_start_activities(self) -> None:
        request = ApprovalWorkflowInput(
            draft_id="11111111-1111-4111-8111-111111111111",
            version=1,
            content_hash="a" * 64,
        )
        valid_approval = DraftApproval(
            approval_id="22222222-2222-4222-8222-222222222222",
            draft_id=request.draft_id,
            version=request.version,
            content_hash=request.content_hash,
            approved_by="Test reviewer",
        )
        cases = (
            (
                "wrong_draft_id",
                replace(
                    valid_approval,
                    draft_id="33333333-3333-4333-8333-333333333333",
                ),
            ),
            ("wrong_version", replace(valid_approval, version=2)),
            ("wrong_content_hash", replace(valid_approval, content_hash="b" * 64)),
            ("empty_approval_id", replace(valid_approval, approval_id="")),
            ("blank_approval_id", replace(valid_approval, approval_id=" \t\n")),
            ("empty_approver", replace(valid_approval, approved_by="")),
            ("blank_approver", replace(valid_approval, approved_by=" \t\n")),
        )

        for case, invalid_approval in cases:
            with self.subTest(case=case):
                instance = ApprovalWorkflow(request)
                approval_result = {"outbox_id": "outbox-test"}
                delivery_result = {
                    "outbox_id": "outbox-test",
                    "state": "sent",
                    "gmail_message_id": "gmail-test",
                    "last_error": None,
                }
                execute_activity = AsyncMock(
                    side_effect=[approval_result, delivery_result]
                )

                async def fake_wait(
                    condition,
                    instance=instance,
                    execute_activity=execute_activity,
                ) -> None:
                    self.assertFalse(condition())
                    execute_activity.assert_not_awaited()
                    instance.approve(valid_approval)
                    self.assertTrue(condition())

                wait_condition = AsyncMock(side_effect=fake_wait)

                with (
                    patch(
                        "sefir_agent.workflows.workflow.wait_condition",
                        wait_condition,
                    ),
                    patch(
                        "sefir_agent.workflows.workflow.execute_activity",
                        execute_activity,
                    ),
                    patch("sefir_agent.workflows.workflow.logger.warning"),
                ):
                    instance.approve(invalid_approval)
                    result = await instance.run(request)

                wait_condition.assert_awaited_once()
                self.assertEqual(result, delivery_result)
                self.assertEqual(execute_activity.await_count, 2)
                self.assertEqual(
                    execute_activity.await_args_list[0].args,
                    ("record_draft_approval", valid_approval),
                )
                self.assertEqual(
                    execute_activity.await_args_list[1].args,
                    ("deliver_outbox_message", "outbox-test"),
                )

    async def test_first_approval_is_preserved(self) -> None:
        request = ApprovalWorkflowInput(
            draft_id="11111111-1111-4111-8111-111111111111",
            version=1,
            content_hash="a" * 64,
        )
        first_approval = DraftApproval(
            approval_id="22222222-2222-4222-8222-222222222222",
            draft_id=request.draft_id,
            version=request.version,
            content_hash=request.content_hash,
            approved_by="Test reviewer",
        )
        cases = (
            ("duplicate", first_approval),
            (
                "different_approval",
                replace(
                    first_approval,
                    approval_id="33333333-3333-4333-8333-333333333333",
                    approved_by="Another reviewer",
                ),
            ),
        )

        for case, second_approval in cases:
            with self.subTest(case=case):
                instance = ApprovalWorkflow(request)
                instance.approve(first_approval)
                instance.approve(second_approval)

                approval_result = {"outbox_id": "outbox-test"}
                delivery_result = {
                    "outbox_id": "outbox-test",
                    "state": "sent",
                    "gmail_message_id": "gmail-test",
                    "last_error": None,
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
                ):
                    result = await instance.run(request)

                self.assertEqual(result, delivery_result)
                self.assertEqual(execute_activity.await_count, 2)
                self.assertEqual(
                    execute_activity.await_args_list[0].args,
                    ("record_draft_approval", first_approval),
                )
                self.assertEqual(
                    execute_activity.await_args_list[1].args,
                    ("deliver_outbox_message", "outbox-test"),
                )
