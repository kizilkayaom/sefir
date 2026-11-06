import asyncio
import json
import os
import sys
from contextlib import redirect_stdout
from typing import Any

from temporalio.client import Client, WorkflowExecutionStatus, WorkflowFailureError
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode

from sefir_agent.providers import ProviderConfigurationError, provider_catalog
from sefir_agent.workflow_types import ApprovalWorkflowInput, DraftApproval


async def workflow_status(
    client: Client, draft_id: str, version: int
) -> dict[str, Any]:
    workflow_id = f"approval-{draft_id}-v{version}"
    handle = client.get_workflow_handle(workflow_id)
    try:
        description = await handle.describe()
    except RPCError as exc:
        if exc.status == RPCStatusCode.NOT_FOUND:
            return {"workflow_id": workflow_id, "status": "NOT_STARTED"}
        raise
    result = {
        "workflow_id": workflow_id,
        "run_id": description.run_id,
        "status": description.status.name if description.status else "UNKNOWN",
        "delivery": None,
        "failure": None,
    }
    terminal = {
        WorkflowExecutionStatus.COMPLETED,
        WorkflowExecutionStatus.FAILED,
        WorkflowExecutionStatus.CANCELED,
        WorkflowExecutionStatus.TERMINATED,
        WorkflowExecutionStatus.TIMED_OUT,
    }
    if description.status in terminal:
        completed = client.get_workflow_handle(
            workflow_id, run_id=description.run_id, result_type=dict[str, Any]
        )
        try:
            result["delivery"] = await completed.result(follow_runs=False)
        except WorkflowFailureError as exc:
            result["failure"] = str(exc.cause or exc)
    return result


async def approve(client: Client, payload: dict[str, Any]) -> dict[str, Any]:
    request = ApprovalWorkflowInput(
        draft_id=payload["draft_id"],
        version=payload["version"],
        content_hash=payload["content_hash"],
    )
    approval = DraftApproval(**payload)
    workflow_id = f"approval-{request.draft_id}-v{request.version}"
    try:
        await client.start_workflow(
            "ApprovalWorkflow",
            request,
            id=workflow_id,
            task_queue="sefir-outreach",
            id_conflict_policy=WorkflowIDConflictPolicy.FAIL,
            id_reuse_policy=WorkflowIDReusePolicy.REJECT_DUPLICATE,
        )
    except WorkflowAlreadyStartedError:
        pass
    handle = client.get_workflow_handle(workflow_id)
    description = await handle.describe()
    if description.status == WorkflowExecutionStatus.RUNNING:
        await handle.signal("approve", approval)
    return await workflow_status(client, request.draft_id, request.version)


async def dispatch(action: str, payload: dict[str, Any]) -> dict[str, Any]:
    if action == "providers":
        return provider_catalog()
    if action == "models":
        from sefir_agent.models import ModelDiscoveryError, list_models

        try:
            return await asyncio.to_thread(list_models, payload["provider"])
        except ModelDiscoveryError as exc:
            return {"error": str(exc)}
    if action == "generate":
        from sefir_agent.drafting import draft_outreach

        try:
            draft = await draft_outreach(
                payload["contact"], payload["cv_text"], payload.get("provider")
            )
        except ProviderConfigurationError:
            return {"error": "provider_unavailable"}
        return draft.model_dump()
    if action not in {"approve", "status", "stop_approval"}:
        raise ValueError("Unknown web operation")
    if action == "stop_approval":
        workflow_id = payload.get("workflow_id")
        if not isinstance(workflow_id, str) or not workflow_id.strip():
            raise ValueError("workflow_id must be a non-empty string")
    client = await Client.connect(os.environ.get("TEMPORAL_ADDRESS", "127.0.0.1:7233"))
    if action == "stop_approval":
        await stop_approval_workflow(client, workflow_id)
        return {"workflow_id": workflow_id, "stopped": True}
    if action == "approve":
        return await approve(client, payload)
    return await workflow_status(client, payload["draft_id"], payload["version"])


def main() -> None:
    try:
        request = json.loads(sys.stdin.read(1_048_577))
        with redirect_stdout(sys.stderr):
            result = asyncio.run(dispatch(request["action"], request["payload"]))
        print(json.dumps(result))
    except (
        KeyError,
        ValueError,
        TypeError,
        OSError,
        RuntimeError,
        RPCError,
        WorkflowFailureError,
    ) as exc:
        print(type(exc).__name__, file=sys.stderr)
        raise SystemExit(1) from None


async def stop_approval_workflow(client: Client, workflow_id: str) -> None:
    handle = client.get_workflow_handle(workflow_id)
    try:
        description = await handle.describe()
        terminal = {
            WorkflowExecutionStatus.COMPLETED,
            WorkflowExecutionStatus.FAILED,
            WorkflowExecutionStatus.CANCELED,
            WorkflowExecutionStatus.TERMINATED,
            WorkflowExecutionStatus.TIMED_OUT,
        }
        if description.status in terminal:
            return
        if description.status != WorkflowExecutionStatus.RUNNING:
            raise RuntimeError("Unknown workflow status")

        working_handle = client.get_workflow_handle(
            workflow_id, run_id=description.run_id
        )
        await working_handle.terminate(reason="Account deletion requested")
    except RPCError as exc:
        if exc.status == RPCStatusCode.NOT_FOUND:
            return
        raise


if __name__ == "__main__":
    main()
