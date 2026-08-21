import unittest

from compressor.service import CompressionService


class FakeEngine:
    def __init__(self) -> None:
        self.calls = []

    def compress_prompt_llmlingua2(self, text, **kwargs):
        self.calls.append((text, kwargs))
        return {
            "compressed_prompt": "short context",
            "origin_tokens": 100,
            "compressed_tokens": 20,
        }


class CompressionServiceTest(unittest.TestCase):
    def test_compresses_batch_with_safe_punctuation_tokens(self):
        engine = FakeEngine()
        results = CompressionService(engine).compress(["long first", "long second"], 0.4)

        self.assertEqual(2, len(results))
        self.assertEqual("short context", results[0].compressed_text)
        self.assertEqual(0.4, engine.calls[0][1]["rate"])
        self.assertIn("\n", engine.calls[0][1]["force_tokens"])

    def test_rejects_invalid_batch_and_ratio(self):
        service = CompressionService(FakeEngine())
        with self.assertRaises(ValueError):
            service.compress([], 0.5)
        with self.assertRaises(ValueError):
            service.compress(["text"], 0.01)


if __name__ == "__main__":
    unittest.main()
