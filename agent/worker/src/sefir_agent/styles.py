from dataclasses import dataclass


@dataclass(frozen=True)
class TonePreset:
    id: str
    name: str
    instruction: str
    revision: int


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
