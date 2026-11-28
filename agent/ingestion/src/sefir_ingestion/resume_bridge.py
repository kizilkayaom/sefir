import json
import resource
import sys
from contextlib import redirect_stderr, redirect_stdout
from dataclasses import asdict

from sefir_ingestion.resume import (
    MAX_PDF_BYTES,
    PDF_COMPLEXITY_ERROR,
    ResumeExtractionError,
    extract_resume_pdf,
)

MAX_PROCESS_BYTES = 512 * 1024 * 1024


def limit_process_memory() -> None:
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    if sys.platform == "darwin":
        return
    if sys.platform != "linux":
        raise RuntimeError("Unsupported extraction platform")
    soft, hard = resource.getrlimit(resource.RLIMIT_AS)
    limits = [MAX_PROCESS_BYTES]
    limits.extend(limit for limit in (soft, hard) if limit != resource.RLIM_INFINITY)
    limit = min(limits)
    resource.setrlimit(resource.RLIMIT_AS, (limit, limit))


class _DiscardOutput:
    def write(self, text: str) -> int:
        return len(text)

    def flush(self) -> None:
        pass


def main() -> None:
    exit_code = 0
    try:
        with redirect_stdout(_DiscardOutput()), redirect_stderr(_DiscardOutput()):
            limit_process_memory()
            data = sys.stdin.buffer.read(MAX_PDF_BYTES + 1)
            result = asdict(extract_resume_pdf(data))
    except ResumeExtractionError as exc:
        result = {"error": "invalid_pdf", "message": str(exc)}
    except MemoryError:
        result = {"error": "invalid_pdf", "message": PDF_COMPLEXITY_ERROR}
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
