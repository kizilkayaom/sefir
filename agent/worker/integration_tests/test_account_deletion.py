import asyncio
import shutil
import unittest

from temporalio import workflow
from temporalio.testing import WorkflowEnvironment
from temporalio.worker import Worker

from sefir_agent.web_bridge import (
    approval_run_deleted,
    list_approval_runs,
    request_approval_run_deletion,
    stop_approval_workflow,
)


@workflow.defn
class DeletionChain:
    @workflow.run
    async def run(self, continue_once: bool) -> None:
        if continue_once:
            workflow.continue_as_new(False)
        await workflow.wait_condition(lambda: False)


class AccountDeletionIntegrationTests(unittest.IsolatedAsyncioTestCase):
    async def test_stops_and_deletes_all_runs_including_continued_history(self) -> None:
        temporal_path = shutil.which("temporal")
        self.assertIsNotNone(temporal_path, "Temporal CLI is required")
        workflow_id = "approval-11111111-1111-4111-8111-111111111111-v1"
        async with (
            await WorkflowEnvironment.start_local(
                dev_server_existing_path=temporal_path,
                dev_server_extra_args=["--disable-config-env", "--disable-config-file"],
            ) as environment,
            Worker(
                environment.client,
                task_queue="deletion-test",
                workflows=[DeletionChain],
            ),
        ):
            client = environment.client
            handle = await client.start_workflow(
                DeletionChain.run, True, id=workflow_id, task_queue="deletion-test"
            )
            first_run = handle.first_execution_run_id
            async with asyncio.timeout(30):
                while (
                    await client.get_workflow_handle(workflow_id).describe()
                ).run_id == first_run:
                    await asyncio.sleep(0.1)
            await stop_approval_workflow(client, workflow_id)
            runs = await list_approval_runs(client, workflow_id)
            self.assertEqual(len(runs), 2)
            self.assertIn(first_run, runs)
            for run_id in runs:
                self.assertFalse(
                    await approval_run_deleted(client, workflow_id, run_id)
                )
                await request_approval_run_deletion(client, workflow_id, run_id)
            async with asyncio.timeout(120):
                for run_id in runs:
                    while not await approval_run_deleted(client, workflow_id, run_id):
                        await asyncio.sleep(0.1)
                    await request_approval_run_deletion(client, workflow_id, run_id)
