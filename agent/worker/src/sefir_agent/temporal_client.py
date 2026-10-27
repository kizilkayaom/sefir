from temporalio.client import Client
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy

from sefir_agent.workflow_types import ApprovalWorkflowInput, DraftApproval
from sefir_agent.workflows import ApprovalWorkflow


async def start_approval_workflow(
    client: Client,
    request: ApprovalWorkflowInput,
) -> str:
    workflow_id = f"approval-{request.draft_id}-v{request.version}"
    handle = await client.start_workflow(
        ApprovalWorkflow.run,
        request,
        id=workflow_id,
        task_queue="sefir-outreach",
        id_conflict_policy=WorkflowIDConflictPolicy.FAIL,
        id_reuse_policy=WorkflowIDReusePolicy.REJECT_DUPLICATE,
    )

    return handle.id


async def signal_workflow_approval(
    client: Client,
    approval: DraftApproval,
) -> None:
    workflow_id = f"approval-{approval.draft_id}-v{approval.version}"
    handle = client.get_workflow_handle(workflow_id)
    await handle.signal(ApprovalWorkflow.approve, approval)


async def get_approval_workflow_status(
    client: Client,
    draft_id: str,
    version: int,
) -> dict[str, str]:
    workflow_id = f"approval-{draft_id}-v{version}"
    handle = client.get_workflow_handle(workflow_id)
    description = await handle.describe()
    return {
        "workflow_id": description.id,
        "run_id": description.run_id,
        "status": description.status.name
        if description.status is not None
        else "UNKNOWN",
    }
