from temporalio import workflow


@workflow.defn
class ApprovalWorkflow:
    def __init__(self) -> None:
        self._approved = False

    @workflow.run
    async def run(self) -> str:
        await workflow.wait_condition(lambda: self._approved)
        return "approved"

    @workflow.signal
    def approve(self) -> None:
        self._approved = True
