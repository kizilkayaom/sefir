import unittest
from dataclasses import asdict

from sefir_agent.styles import parse_custom_profile


class CustomWritingProfileTests(unittest.TestCase):
    def setUp(self) -> None:
        self.profile = {
            "profile_id": "55555555-5555-4555-8555-555555555555",
            "revision": 3,
            "name": 'Sıcak: "doğrudan"',
            "language": "tr",
            "content": "\nKısa cümleler kullan.\r\n\n    Preserve indentation.\n",
        }

    def test_valid_profile_preserves_fields(self) -> None:
        original = self.profile.copy()
        parsed = parse_custom_profile(self.profile)
        self.assertEqual(asdict(parsed), original)
        self.assertEqual(self.profile, original)

    def test_supported_limits(self) -> None:
        for revision in (1, 2147483647):
            with self.subTest(revision=revision):
                value = {
                    **self.profile,
                    "revision": revision,
                    "name": "Ö" * 80,
                    "language": "a" * 35,
                    "content": "ğ" * (8 * 1024),
                }
                self.assertEqual(asdict(parse_custom_profile(value)), value)

    def test_rejects_non_objects(self) -> None:
        for value in (None, [], "profile", 1, False):
            with self.subTest(value=value), self.assertRaises(TypeError):
                parse_custom_profile(value)

    def test_requires_exact_fields(self) -> None:
        for field in self.profile:
            value = self.profile.copy()
            del value[field]
            with self.subTest(missing=field), self.assertRaises(ValueError):
                parse_custom_profile(value)
        with self.assertRaises(ValueError):
            parse_custom_profile({**self.profile, "account_id": "other"})

    def test_rejects_invalid_revisions(self) -> None:
        for revision in (None, True, False, 0, -1, 2147483648, "1", 1.0):
            with self.subTest(revision=revision), self.assertRaises(ValueError):
                parse_custom_profile({**self.profile, "revision": revision})

    def test_rejects_non_string_fields(self) -> None:
        for field in ("profile_id", "name", "language", "content"):
            for value in (None, 1, False, [], {}):
                with (
                    self.subTest(field=field, value=value),
                    self.assertRaises(TypeError),
                ):
                    parse_custom_profile({**self.profile, field: value})

    def test_rejects_blank_nul_and_invalid_unicode_fields(self) -> None:
        for field in ("profile_id", "name", "language", "content"):
            for value in ("", " \t\r\n", "text\x00", "text\ud800"):
                with (
                    self.subTest(field=field, value=value),
                    self.assertRaises(ValueError),
                ):
                    parse_custom_profile({**self.profile, field: value})

    def test_rejects_invalid_uuids(self) -> None:
        for value in (
            "not-a-uuid",
            "gggggggg-5555-4555-8555-555555555555",
            self.profile["profile_id"] + "-extra",
        ):
            with self.subTest(profile_id=value), self.assertRaises(ValueError):
                parse_custom_profile({**self.profile, "profile_id": value})

    def test_rejects_exceeded_limits(self) -> None:
        for field, value in (
            ("name", "Ö" * 81),
            ("language", "a" * 36),
            ("content", "x" * (16 * 1024 + 1)),
            ("content", "ğ" * (8 * 1024 + 1)),
        ):
            with (
                self.subTest(field=field, size=len(value)),
                self.assertRaises(ValueError),
            ):
                parse_custom_profile({**self.profile, field: value})
