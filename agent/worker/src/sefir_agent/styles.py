import uuid
from dataclasses import dataclass


@dataclass(frozen=True)
class TonePreset:
    id: str
    name: str
    instruction: str
    revision: int


@dataclass(frozen=True)
class CustomWritingProfile:
    profile_id: str
    revision: int
    name: str
    language: str
    content: str


prof = TonePreset(
    "prof", "Concise professional", "Keep the tone concise and professional", 1
)
warm_prof = TonePreset(
    "warm_prof", "Warm professional", "Keep the tone professional but warm", 1
)
technical = TonePreset(
    "technical", "Technical and direct", "Keep the tone technical and direct", 1
)
formal = TonePreset("formal", "Formal", "Keep the tone formal", 1)

PRESETS = {preset.id: preset for preset in (prof, warm_prof, technical, formal)}


def get_tone_preset(preset_id: str | None) -> TonePreset:
    if preset_id is None:
        return prof
    if preset_id not in PRESETS:
        raise ValueError(f"Unknown preset ID: {preset_id}")
    return PRESETS[preset_id]


def parse_custom_profile(value: object) -> CustomWritingProfile:
    if not isinstance(value, dict):
        raise TypeError("Custom profile must be an object.")

    expected_fields = {
        "profile_id",
        "revision",
        "name",
        "language",
        "content",
    }
    if set(value) != expected_fields:
        raise ValueError(
            "Custom profile must contain exactly profile_id, revision, name, language, and content."
        )

    profile_id = value["profile_id"]
    revision = value["revision"]
    name = value["name"]
    language = value["language"]
    content = value["content"]

    if type(revision) is not int or not 1 <= revision <= 2147483647:
        raise ValueError(
            "Profile revision must be an integer between 1 and 2147483647."
        )
    for field, text in (
        ("profile_id", profile_id),
        ("name", name),
        ("language", language),
        ("content", content),
    ):
        if not isinstance(text, str):
            raise TypeError(f"Profile {field} must be a string.")
        if not text.strip():
            raise ValueError(f"Profile {field} must not be blank.")
        if "\x00" in text:
            raise ValueError(f"Profile {field} must not contain NUL characters.")
        try:
            text.encode("utf-8")
        except UnicodeEncodeError as exc:
            raise ValueError(f"Profile {field} must contain valid UTF-8.") from exc
    try:
        uuid.UUID(profile_id)
    except ValueError as exc:
        raise ValueError("Profile ID must be a valid UUID.") from exc

    if len(name) > 80:
        raise ValueError("Profile name must not exceed 80 characters.")
    if len(language) > 35:
        raise ValueError("Profile language must not exceed 35 characters.")
    if len(content.encode("utf-8")) > 16 * 1024:
        raise ValueError("Profile content must not exceed 16 KiB.")
    writing_profile = CustomWritingProfile(
        profile_id, revision, name, language, content
    )
    return writing_profile
