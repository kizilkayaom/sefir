from dataclasses import dataclass


@dataclass(frozen=True)
class ApprovalWorkflowInput:
    draft_id: str
    version: int
    content_hash: str


@dataclass(frozen=True)
class DraftApproval:
    approval_id: str
    draft_id: str
    version: int
    content_hash: str
    approved_by: str
