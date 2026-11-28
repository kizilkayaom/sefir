import unittest
from dataclasses import FrozenInstanceError
from io import BytesIO
from unittest.mock import patch

from pypdf import PdfReader, PdfWriter, get_configuration
from pypdf.errors import PdfReadError
from pypdf.generic import DecodedStreamObject, DictionaryObject, NameObject

from sefir_ingestion.resume import (
    PDF_COMPLEXITY_ERROR,
    ExtractedResume,
    ResumeExtractionError,
    extract_resume_pdf,
)


def make_pdf(pages: list[str], password: str | None = None) -> bytes:
    with PdfWriter() as writer, BytesIO() as stream:
        font = DictionaryObject(
            {
                NameObject("/Type"): NameObject("/Font"),
                NameObject("/Subtype"): NameObject("/Type1"),
                NameObject("/BaseFont"): NameObject("/Helvetica"),
                NameObject("/Encoding"): NameObject("/WinAnsiEncoding"),
            }
        )
        for text in pages:
            page = writer.add_blank_page(width=612, height=792)
            if text:
                page[NameObject("/Resources")] = DictionaryObject(
                    {NameObject("/Font"): DictionaryObject({NameObject("/F1"): font})}
                )
                contents = DecodedStreamObject()
                encoded = text.encode("cp1252").hex().encode("ascii")
                contents.set_data(b"BT /F1 12 Tf 72 720 Td <" + encoded + b"> Tj ET")
                page[NameObject("/Contents")] = contents
        if password is not None:
            writer.encrypt(password)
        writer.write(stream)
        return stream.getvalue()


def make_compressed_pdf(sizes: list[int]) -> bytes:
    with (
        PdfReader(
            BytesIO(make_pdf([f"Fictional page {i}" for i in range(len(sizes))]))
        ) as reader,
        PdfWriter() as writer,
        BytesIO() as stream,
    ):
        writer.append(reader)
        for page, size in zip(writer.pages, sizes, strict=True):
            contents = page["/Contents"].get_object()
            original = contents.get_data()
            contents.set_data(b" " * (size - len(original)) + original)
            page[NameObject("/Contents")] = writer._add_object(contents.flate_encode())
        writer.compress_identical_objects()
        writer.write(stream)
        return stream.getvalue()


class ResumeExtractionTests(unittest.TestCase):
    def test_compressed_content_limits_and_configuration_restoration(self) -> None:
        configuration = get_configuration()
        with (
            patch("sefir_ingestion.resume.MAX_STREAM_BYTES", 2048),
            patch("sefir_ingestion.resume.MAX_CONTENT_BYTES", 4096),
        ):
            result = extract_resume_pdf(make_compressed_pdf([2048, 2048]))
            self.assertEqual(result.text, "Fictional page 0\n\nFictional page 1")
            self.assertIs(get_configuration(), configuration)

            for sizes in ([2049], [1500, 1500, 1500]):
                with self.subTest(sizes=sizes):
                    with self.assertRaises(ResumeExtractionError) as raised:
                        extract_resume_pdf(make_compressed_pdf(sizes))
                    self.assertEqual(str(raised.exception), PDF_COMPLEXITY_ERROR)
                    self.assertIs(get_configuration(), configuration)

    def test_memory_exhaustion_returns_safe_validation_error(self) -> None:
        with patch(
            "pypdf.PageObject.extract_text", side_effect=MemoryError("private details")
        ):
            with self.assertRaises(ResumeExtractionError) as raised:
                extract_resume_pdf(make_pdf(["Fictional résumé"]))
        self.assertEqual(str(raised.exception), PDF_COMPLEXITY_ERROR)

    def test_extracts_pages_in_order_and_preserves_facts(self) -> None:
        first = "Alex Example: 3 years of Go experience."
        second = "Available 15 January 2027. https://example.com/portfolio"
        data = make_pdf([first, "", second])
        original = data[:]

        result = extract_resume_pdf(data)

        self.assertEqual(result, ExtractedResume(first + "\n\n" + second, 3))
        self.assertEqual(data, original)
        with self.assertRaises(FrozenInstanceError):
            result.text = "Changed"

    def test_rejects_empty_oversized_and_non_byte_input_before_parsing(self) -> None:
        with patch("sefir_ingestion.resume.PdfReader") as reader:
            for data, message in (
                (b"", "PDF file must not be empty."),
                (b"x" * (5 * 1024 * 1024 + 1), "PDF file must not exceed 5 MiB."),
                ("resume.pdf", "PDF data must be bytes."),
                (None, "PDF data must be bytes."),
            ):
                with self.subTest(message=message):
                    with self.assertRaises(ResumeExtractionError) as raised:
                        extract_resume_pdf(data)
                    self.assertEqual(str(raised.exception), message)
            reader.assert_not_called()

    def test_accepts_file_size_limit(self) -> None:
        with PdfWriter() as writer, BytesIO() as stream:
            with BytesIO(make_pdf(["Fictional résumé"])) as source:
                writer.append(source)
            padding = 0
            for _ in range(3):
                writer.add_metadata({"/Title": "x" * padding})
                stream.seek(0)
                stream.truncate()
                writer.write(stream)
                data = stream.getvalue()
                if len(data) == 5 * 1024 * 1024:
                    break
                padding += 5 * 1024 * 1024 - len(data)

        self.assertEqual(len(data), 5 * 1024 * 1024)
        self.assertEqual(
            extract_resume_pdf(data), ExtractedResume("Fictional résumé", 1)
        )

    def test_rejects_malformed_pdfs(self) -> None:
        valid = make_pdf(["Fictional résumé"])
        for data in (b"Not a PDF", b"%PDF-1.7\n", valid[: len(valid) // 2]):
            with self.subTest(data=data[:20]):
                with self.assertRaises(ResumeExtractionError) as raised:
                    extract_resume_pdf(data)
                self.assertEqual(
                    str(raised.exception),
                    "Could not read this PDF. Upload a valid PDF or paste your résumé text.",
                )

    def test_rejects_encrypted_pdfs_even_with_empty_password(self) -> None:
        for password in ("fictional-password", ""):
            with self.subTest(password=password):
                with self.assertRaises(ResumeExtractionError) as raised:
                    extract_resume_pdf(make_pdf(["Fictional résumé"], password))
                self.assertEqual(
                    str(raised.exception),
                    "Encrypted PDFs are not supported. Upload an unencrypted PDF.",
                )

    def test_enforces_page_count_limits(self) -> None:
        for count in (0, 21):
            with self.subTest(count=count):
                with self.assertRaises(ResumeExtractionError) as raised:
                    extract_resume_pdf(make_pdf(["Fictional résumé"] * count))
                self.assertEqual(
                    str(raised.exception), "PDF must contain between 1 and 20 pages."
                )

        result = extract_resume_pdf(make_pdf(["Fictional résumé"] + [""] * 19))
        self.assertEqual(result, ExtractedResume("Fictional résumé", 20))

    def test_rejects_documents_without_readable_text(self) -> None:
        for pages in ([""], ["   ", ""]):
            with self.subTest(pages=pages):
                with self.assertRaises(ResumeExtractionError) as raised:
                    extract_resume_pdf(make_pdf(pages))
                self.assertEqual(
                    str(raised.exception),
                    "No readable text found. Upload a PDF with selectable text "
                    "or paste your résumé text.",
                )

    def test_text_limit_counts_utf8_bytes_and_page_separators(self) -> None:
        for pages in (["é" * 50_000], ["é" * 24_999, "é" * 25_000]):
            with self.subTest(lengths=[len(text) for text in pages]):
                result = extract_resume_pdf(make_pdf(pages))
                self.assertEqual(result.text, "\n\n".join(pages))
                self.assertEqual(len(result.text.encode("utf-8")), 100_000)
                self.assertEqual(result.page_count, len(pages))

        for pages in (["é" * 50_001], ["é" * 25_000, "é" * 25_000]):
            with self.subTest(lengths=[len(text) for text in pages]):
                with self.assertRaises(ResumeExtractionError) as raised:
                    extract_resume_pdf(make_pdf(pages))
                self.assertEqual(
                    str(raised.exception),
                    "Extracted résumé text must not exceed 100,000 UTF-8 bytes.",
                )

    def test_parser_failures_do_not_expose_document_details(self) -> None:
        data = make_pdf(["Fictional résumé"])
        for error in (PdfReadError("private PDF content"), ValueError("private font")):
            with self.subTest(error=type(error).__name__):
                with (
                    patch("pypdf.PageObject.extract_text", side_effect=error),
                    self.assertRaises(ResumeExtractionError) as raised,
                ):
                    extract_resume_pdf(data)
                self.assertEqual(
                    str(raised.exception),
                    "Could not read this PDF. Upload a valid PDF or paste your résumé text.",
                )
                self.assertTrue(raised.exception.__suppress_context__)
