from typing import Any

from temporalio import activity

from sefir_agent.email import approve_draft, send_outbox_message
from sefir_agent.workflow_types import DraftApproval


@activity.defn
async def record_draft_approval(approval: DraftApproval) -> dict[str, Any]:
    return await approve_draft(
        approval_id=approval.approval_id,
        draft_id=approval.draft_id,
        version=approval.version,
        content_hash=approval.content_hash,
        approved_by=approval.approved_by,
    )


@activity.defn
async def deliver_outbox_message(outbox_id: str) -> dict[str, Any]:
    return await send_outbox_message(outbox_id=outbox_id)
