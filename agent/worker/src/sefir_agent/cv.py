from pathlib import Path


def load_cv(path: Path) -> str:
    text = path.expanduser().read_text(encoding="utf-8").strip()
    if not text:
        raise ValueError("CV file is empty")
    return text
