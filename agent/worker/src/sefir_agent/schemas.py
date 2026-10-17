from pydantic import BaseModel, ConfigDict, Field


class EmailDraft(BaseModel):
    subject: str = Field(min_length=1)
    body: str = Field(min_length=1)

    model_config = ConfigDict(
        str_strip_whitespace=True,
        extra="forbid",
    )
