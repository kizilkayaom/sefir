import json
import sys
from contextlib import redirect_stderr, redirect_stdout
from dataclasses import asdict

from sefir_ingestion.resume import (
    MAX_PDF_BYTES,
    ResumeExtractionError,
    extract_resume_pdf,
)


class _DiscardOutput:
    def write(self, text: str) -> int:
        return len(text)

    def flush(self) -> None:
        pass


def main() -> None:
    exit_code = 0
    try:
        with redirect_stdout(_DiscardOutput()), redirect_stderr(_DiscardOutput()):
            data = sys.stdin.buffer.read(MAX_PDF_BYTES + 1)
            result = asdict(extract_resume_pdf(data))
    except ResumeExtractionError as exc:
        result = {"error": "invalid_pdf", "message": str(exc)}
    except Exception:
        result = {
            "error": "extraction_failed",
            "message": "Could not extract résumé text. Please try again.",
        }
        exit_code = 1

    print(json.dumps(result))
    if exit_code:
        raise SystemExit(exit_code) from None


if __name__ == "__main__":
    main()
