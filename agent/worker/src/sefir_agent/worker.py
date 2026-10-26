import asyncio

from temporalio.client import Client
from temporalio.worker import Worker

from sefir_agent.workflows import ApprovalWorkflow


async def run_worker() -> None:
    client = await Client.connect("localhost:7233")
    worker = Worker(
        client,
        task_queue="sefir-outreach",
        workflows=[ApprovalWorkflow],
    )
    await worker.run()


def main() -> None:
    asyncio.run(run_worker())
