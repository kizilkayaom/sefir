from datetime import timedelta
from typing import Any

from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.exceptions import ApplicationError

from sefir_agent.workflow_types import ApprovalWorkflowInput, DraftApproval


@workflow.defn
class ApprovalWorkflow:
    @workflow.init
    def __init__(self, request: ApprovalWorkflowInput) -> None:
        self._request = request
        self._approval: DraftApproval | None = None

    @workflow.run
    async def run(self, request: ApprovalWorkflowInput) -> dict[str, Any]:
        await workflow.wait_condition(lambda: self._approval is not None)
        assert self._approval is not None
        approval = await workflow.execute_activity(
            "record_draft_approval",
            self._approval,
            result_type=dict[str, Any],
            start_to_close_timeout=timedelta(seconds=60),
            schedule_to_close_timeout=timedelta(minutes=5),
            retry_policy=RetryPolicy(maximum_attempts=3),
        )
        delivery = await workflow.execute_activity(
            "deliver_outbox_message",
            approval["outbox_id"],
            result_type=dict[str, Any],
            start_to_close_timeout=timedelta(minutes=2),
            schedule_to_close_timeout=timedelta(minutes=5),
            retry_policy=RetryPolicy(maximum_attempts=3),
        )
        state = delivery.get("state")
        if state != "sent":
            message = (
                f"Delivery not confirmed for outbox {approval['outbox_id']} "
                f"(state: {state})"
            )
            if delivery.get("last_error"):
                message += f": {delivery['last_error']}"
            raise ApplicationError(
                message,
                delivery,
                type="DeliveryNotConfirmed",
                non_retryable=True,
            )
        return delivery

    @workflow.signal
    def approve(self, approval: DraftApproval) -> None:
        if self._approval is not None:
            return

        if (
            approval.draft_id != self._request.draft_id
            or approval.version != self._request.version
            or approval.content_hash != self._request.content_hash
        ):
            workflow.logger.warning(
                "Ignoring approval that does not match the workflow draft"
            )
            return

        if not approval.approval_id.strip() or not approval.approved_by.strip():
            workflow.logger.warning(
                "Ignoring approval with a blank approval ID or approver"
            )
            return

        self._approval = approval
