import json
import os
import subprocess
import sys
import unittest
from io import BytesIO, StringIO
from types import SimpleNamespace
from unittest.mock import patch

from test_resume import make_pdf

from sefir_ingestion.resume import MAX_PDF_BYTES, ExtractedResume, ResumeExtractionError
from sefir_ingestion.resume_bridge import main


class ResumeBridgeTests(unittest.TestCase):
    def invoke(self, stream) -> tuple[int, dict, str]:
        stdout, stderr = StringIO(), StringIO()
        exit_code = 0
        with (
            patch("sys.stdin", SimpleNamespace(buffer=stream)),
            patch("sys.stdout", stdout),
            patch("sys.stderr", stderr),
        ):
            try:
                main()
            except SystemExit as exc:
                exit_code = exc.code
        self.assertEqual(len(stdout.getvalue().splitlines()), 1)
        return exit_code, json.loads(stdout.getvalue()), stderr.getvalue()

    def run_module(self, data: bytes) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, "-m", "sefir_ingestion.resume_bridge"],
            input=data,
            capture_output=True,
            check=False,
            timeout=15,
            env={
                "PATH": os.defpath,
                "PYTHON_DOTENV_DISABLED": "1",
                "PYTHONUTF8": "1",
                "PYTHONDONTWRITEBYTECODE": "1",
            },
        )

    def test_module_extracts_synthetic_pdf(self) -> None:
        pages = ['Alex Example: "Résumé"', "3 years of Go experience."]

        result = self.run_module(make_pdf(pages))

        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stderr, b"")
        self.assertEqual(len(result.stdout.splitlines()), 1)
        self.assertEqual(
            json.loads(result.stdout), {"text": "\n\n".join(pages), "page_count": 2}
        )

    def test_module_returns_validation_errors_with_normal_exit(self) -> None:
        for data, message in (
            (b"", "PDF file must not be empty."),
            (
                b"Not a PDF: fictional private document contents",
                "Could not read this PDF. Upload a valid PDF or paste your résumé text.",
            ),
            (
                make_pdf(["Fictional résumé"], "test-password"),
                "Encrypted PDFs are not supported. Upload an unencrypted PDF.",
            ),
        ):
            with self.subTest(message=message):
                result = self.run_module(data)

                self.assertEqual(result.returncode, 0)
                self.assertEqual(result.stderr, b"")
                self.assertEqual(len(result.stdout.splitlines()), 1)
                self.assertEqual(
                    json.loads(result.stdout),
                    {"error": "invalid_pdf", "message": message},
                )

    def test_input_read_is_bounded_and_oversized_data_is_rejected(self) -> None:
        stream = BytesIO(b"x" * (MAX_PDF_BYTES + 100))

        exit_code, result, stderr = self.invoke(stream)

        self.assertEqual(stream.tell(), MAX_PDF_BYTES + 1)
        self.assertEqual(exit_code, 0)
        self.assertEqual(
            result,
            {"error": "invalid_pdf", "message": "PDF file must not exceed 5 MiB."},
        )
        self.assertEqual(stderr, "")

    def test_success_suppresses_parser_diagnostics(self) -> None:
        data = b"synthetic PDF input"

        def extract(value):
            print("Private parser stdout")
            print("Private parser stderr", file=sys.stderr)
            return ExtractedResume("Reviewed résumé\nSecond line", 2)

        with patch(
            "sefir_ingestion.resume_bridge.extract_resume_pdf", side_effect=extract
        ) as extractor:
            exit_code, result, stderr = self.invoke(BytesIO(data))

        self.assertEqual(exit_code, 0)
        self.assertEqual(
            result, {"text": "Reviewed résumé\nSecond line", "page_count": 2}
        )
        self.assertEqual(stderr, "")
        extractor.assert_called_once_with(data)

    def test_validation_error_suppresses_parser_diagnostics(self) -> None:
        def extract(data):
            print("Private parser stdout")
            print("Private parser stderr", file=sys.stderr)
            raise ResumeExtractionError("PDF must contain between 1 and 20 pages.")

        with patch(
            "sefir_ingestion.resume_bridge.extract_resume_pdf", side_effect=extract
        ):
            exit_code, result, stderr = self.invoke(BytesIO(b"synthetic PDF input"))

        self.assertEqual(exit_code, 0)
        self.assertEqual(
            result,
            {
                "error": "invalid_pdf",
                "message": "PDF must contain between 1 and 20 pages.",
            },
        )
        self.assertEqual(stderr, "")

    def test_unexpected_extraction_error_returns_generic_error_and_exit_one(
        self,
    ) -> None:
        def extract(data):
            print("Private parser stdout")
            print("Private parser stderr", file=sys.stderr)
            raise RuntimeError("Fictional private document details")

        with patch(
            "sefir_ingestion.resume_bridge.extract_resume_pdf", side_effect=extract
        ):
            exit_code, result, stderr = self.invoke(BytesIO(b"synthetic PDF input"))

        self.assertEqual(exit_code, 1)
        self.assertEqual(
            result,
            {
                "error": "extraction_failed",
                "message": "Could not extract résumé text. Please try again.",
            },
        )
        self.assertEqual(stderr, "")

    def test_input_read_failure_returns_generic_error_and_exit_one(self) -> None:
        stream = BytesIO()
        stream.close()

        exit_code, result, stderr = self.invoke(stream)

        self.assertEqual(exit_code, 1)
        self.assertEqual(
            result,
            {
                "error": "extraction_failed",
                "message": "Could not extract résumé text. Please try again.",
            },
        )
        self.assertEqual(stderr, "")
