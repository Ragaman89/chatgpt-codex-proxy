from __future__ import annotations

import os
import unittest

from compressor.app import load_service


class MultilingualModelIntegrationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        model_name = os.getenv("MODEL_NAME", "")
        if not model_name or not os.path.isdir(model_name):
            raise unittest.SkipTest("MODEL_NAME does not point to an embedded model")
        cls.service = load_service()

    def assert_compresses(self, text: str, anchors: tuple[str, ...]) -> None:
        result = self.service.compress([text], 0.5)[0]
        self.assertTrue(result.compressed_text.strip())
        self.assertGreater(result.original_tokens, result.compressed_tokens)
        self.assertLess(result.compressed_tokens, result.original_tokens * 0.8)
        normalized = result.compressed_text.casefold()
        self.assertTrue(
            any(anchor.casefold() in normalized for anchor in anchors),
            f"compressed text lost all domain anchors: {result.compressed_text!r}",
        )

    def test_compresses_german_prose(self) -> None:
        self.assert_compresses(
            "Die Gartenbewässerung soll nur dann eingeschaltet werden, wenn "
            "mehrere trockene und sonnige Tage erwartet werden. Der aktuelle "
            "Zustand des Wassertanks muss dabei berücksichtigt werden. Nach "
            "einer erfolgreichen Bewässerung wird der Zeitpunkt für Rasen, "
            "Obstbäume und Pflanzen getrennt gespeichert.",
            ("Gartenbewässerung", "Wassertank", "Rasen", "Obstbäume"),
        )

    def test_compresses_english_prose(self) -> None:
        self.assert_compresses(
            "Garden irrigation should only be enabled when several dry and "
            "sunny days are expected. The current rainwater tank level must "
            "be considered before suggesting the task. After watering, the "
            "timestamp is stored separately for lawns, fruit trees, and plants.",
            ("irrigation", "rainwater", "tank", "lawns", "trees"),
        )


if __name__ == "__main__":
    unittest.main()
