from __future__ import annotations

from dataclasses import dataclass
from threading import Lock
from typing import Any, Protocol


class CompressionEngine(Protocol):
    def compress_prompt_llmlingua2(self, text: str, **kwargs: Any) -> dict[str, Any]: ...


@dataclass(frozen=True)
class CompressionResult:
    compressed_text: str
    original_tokens: int
    compressed_tokens: int


class CompressionService:
    """Serializes inference because the shared PyTorch model is CPU-bound."""

    def __init__(self, engine: CompressionEngine) -> None:
        self._engine = engine
        self._lock = Lock()

    def compress(self, texts: list[str], target_ratio: float) -> list[CompressionResult]:
        if not texts or len(texts) > 16:
            raise ValueError("texts must contain between 1 and 16 entries")
        if not 0.05 <= target_ratio <= 1.0:
            raise ValueError("target_ratio must be between 0.05 and 1.0")
        if any(not isinstance(text, str) or not text.strip() for text in texts):
            raise ValueError("every text must be a non-empty string")
        if sum(len(text) for text in texts) > 2_000_000:
            raise ValueError("combined text length exceeds 2,000,000 characters")

        results: list[CompressionResult] = []
        with self._lock:
            for text in texts:
                raw = self._engine.compress_prompt_llmlingua2(
                    text,
                    rate=target_ratio,
                    force_tokens=["\n", ".", "!", "?", ","],
                    chunk_end_tokens=[".", "\n"],
                    drop_consecutive=True,
                )
                compressed = str(raw.get("compressed_prompt", "")).strip()
                if not compressed:
                    raise RuntimeError("LLMLingua returned an empty compressed prompt")
                results.append(
                    CompressionResult(
                        compressed_text=compressed,
                        original_tokens=int(raw.get("origin_tokens", 0)),
                        compressed_tokens=int(raw.get("compressed_tokens", 0)),
                    )
                )
        return results
