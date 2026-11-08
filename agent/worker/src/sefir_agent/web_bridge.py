import asyncio
import json
import os
import re
import sys
from contextlib import redirect_stdout
from typing import Any

from temporalio.api.common.v1 import WorkflowExecution
from temporalio.api.workflowservice.v1 import DeleteWorkflowExecutionRequest
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
    if action not in {
        "approve",
        "status",
        "stop_approval",
        "request_approval_run_deletion",
        "approval_run_deleted",
        "list_approval_runs",
    }:
        raise ValueError("Unknown web operation")
    if action in {
        "stop_approval",
        "request_approval_run_deletion",
        "approval_run_deleted",
        "list_approval_runs",
    }:
        workflow_id = payload.get("workflow_id")
        if not isinstance(workflow_id, str) or not workflow_id.strip():
            raise ValueError("workflow_id must be a non-empty string")
    if action == "list_approval_runs":
        validate_approval_workflow_id(workflow_id)
    if action in {"request_approval_run_deletion", "approval_run_deleted"}:
        run_id = payload.get("run_id")
        if not isinstance(run_id, str) or not run_id.strip():
            raise ValueError("run_id must be a non-empty string")
    client = await Client.connect(os.environ.get("TEMPORAL_ADDRESS", "127.0.0.1:7233"))
    if action == "stop_approval":
        await stop_approval_workflow(client, workflow_id)
        return {"workflow_id": workflow_id, "stopped": True}
    if action == "request_approval_run_deletion":
        await request_approval_run_deletion(client, workflow_id, run_id)
        return {
            "workflow_id": workflow_id,
            "run_id": run_id,
            "deletion_requested": True,
        }
    if action == "approval_run_deleted":
        deleted = await approval_run_deleted(client, workflow_id, run_id)
        return {"workflow_id": workflow_id, "run_id": run_id, "deleted": deleted}
    if action == "list_approval_runs":
        run_ids = await list_approval_runs(client, workflow_id)
        return {"workflow_id": workflow_id, "run_ids": run_ids}
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


async def request_approval_run_deletion(
    client: Client, workflow_id: str, run_id: str
) -> None:
    if not isinstance(workflow_id, str) or not workflow_id.strip():
        raise ValueError("workflow_id must be a non-empty string")
    if not isinstance(run_id, str) or not run_id.strip():
        raise ValueError("run_id must be a non-empty string")

    handle = client.get_workflow_handle(workflow_id, run_id=run_id)
    try:
        description = await handle.describe()
        terminal = {
            WorkflowExecutionStatus.COMPLETED,
            WorkflowExecutionStatus.FAILED,
            WorkflowExecutionStatus.CANCELED,
            WorkflowExecutionStatus.TERMINATED,
            WorkflowExecutionStatus.TIMED_OUT,
        }
        if description.status not in terminal:
            raise RuntimeError("Workflow run must be closed before deletion")

        await client.workflow_service.delete_workflow_execution(
            DeleteWorkflowExecutionRequest(
                namespace=client.namespace,
                workflow_execution=WorkflowExecution(
                    workflow_id=workflow_id, run_id=run_id
                ),
            )
        )
    except RPCError as exc:
        if exc.status == RPCStatusCode.NOT_FOUND:
            return
        raise


async def approval_run_deleted(client: Client, workflow_id: str, run_id: str) -> bool:
    if not isinstance(workflow_id, str) or not workflow_id.strip():
        raise ValueError("workflow_id must be a non-empty string")
    if not isinstance(run_id, str) or not run_id.strip():
        raise ValueError("run_id must be a non-empty string")

    handle = client.get_workflow_handle(workflow_id, run_id=run_id)
    try:
        await handle.describe()
    except RPCError as exc:
        if exc.status != RPCStatusCode.NOT_FOUND:
            raise
    else:
        return False

    try:
        events = handle.fetch_history_events(page_size=1)
        await anext(events, None)
    except RPCError as exc:
        if exc.status == RPCStatusCode.NOT_FOUND:
            return True
        raise
    return False


def validate_approval_workflow_id(workflow_id: object) -> None:
    if not isinstance(workflow_id, str) or not re.fullmatch(
        r"approval-[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-"
        r"[0-9a-fA-F]{4}-[0-9a-fA-F]{12}-v[1-9][0-9]*",
        workflow_id,
    ):
        raise ValueError("workflow_id must match approval-<UUID>-v<positive integer>")


async def list_approval_runs(client: Client, workflow_id: str) -> list[str]:
    validate_approval_workflow_id(workflow_id)
    run_ids: set[str] = set()
    handle = client.get_workflow_handle(workflow_id)
    try:
        description = await handle.describe()
    except RPCError as exc:
        if exc.status != RPCStatusCode.NOT_FOUND:
            raise
    else:
        if not isinstance(description.run_id, str) or not description.run_id.strip():
            raise RuntimeError("Temporal returned an invalid workflow run ID")
        run_ids.add(description.run_id)

    query = f"WorkflowId = '{workflow_id}'"
    async for execution in client.list_workflows(query=query):
        if execution.id != workflow_id:
            raise RuntimeError("Temporal returned an unexpected workflow ID")
        if not isinstance(execution.run_id, str) or not execution.run_id.strip():
            raise RuntimeError("Temporal returned an invalid workflow run ID")
        run_ids.add(execution.run_id)
    return sorted(run_ids)


if __name__ == "__main__":
    main()
