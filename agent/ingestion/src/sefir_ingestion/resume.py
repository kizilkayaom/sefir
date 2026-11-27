from dataclasses import dataclass
from io import BytesIO

from pypdf import PdfReader
from pypdf.errors import DependencyError, PyPdfError

MAX_PDF_BYTES = 5 * 1024 * 1024
MAX_PDF_PAGES = 20
MAX_TEXT_BYTES = 100_000


@dataclass(frozen=True)
class ExtractedResume:
    text: str
    page_count: int


class ResumeExtractionError(ValueError):
    pass


def extract_resume_pdf(data: bytes) -> ExtractedResume:
    if not isinstance(data, bytes):
        raise ResumeExtractionError("PDF data must be bytes.")
    if not data:
        raise ResumeExtractionError("PDF file must not be empty.")
    if len(data) > MAX_PDF_BYTES:
        raise ResumeExtractionError("PDF file must not exceed 5 MiB.")

    try:
        with BytesIO(data) as stream, PdfReader(stream, strict=True) as reader:
            if reader.is_encrypted:
                raise ResumeExtractionError(
                    "Encrypted PDFs are not supported. Upload an unencrypted PDF."
                )
            page_count = len(reader.pages)
            if not 1 <= page_count <= MAX_PDF_PAGES:
                raise ResumeExtractionError("PDF must contain between 1 and 20 pages.")

            pages = []
            text_bytes = 0
            for page in reader.pages:
                text = (page.extract_text() or "").strip()
                if not text:
                    continue
                text_bytes += len(text.encode("utf-8")) + (2 if pages else 0)
                if text_bytes > MAX_TEXT_BYTES:
                    raise ResumeExtractionError(
                        "Extracted résumé text must not exceed 100,000 UTF-8 bytes."
                    )
                pages.append(text)

            if not pages:
                raise ResumeExtractionError(
                    "No readable text found. Upload a PDF with selectable text "
                    "or paste your résumé text."
                )
            return ExtractedResume(text="\n\n".join(pages), page_count=page_count)
    except ResumeExtractionError:
        raise
    except (
        PyPdfError,
        DependencyError,
        ValueError,
        TypeError,
        KeyError,
        IndexError,
        RecursionError,
    ):
        raise ResumeExtractionError(
            "Could not read this PDF. Upload a valid PDF or paste your résumé text."
        ) from None
