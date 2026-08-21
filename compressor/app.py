from __future__ import annotations

import json
import logging
import os
import time
import traceback
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from compressor.service import CompressionService


MAX_REQUEST_BYTES = 8 << 20


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "time": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(record.created)),
            "level": record.levelname.lower(),
            "message": record.getMessage(),
        }
        for key in ("path", "status", "latency_ms", "batch_size", "error", "error_type"):
            if hasattr(record, key):
                payload[key] = getattr(record, key)
        if record.exc_info:
            payload["stack_trace"] = "".join(traceback.format_exception(*record.exc_info)).rstrip()
        return json.dumps(payload, ensure_ascii=False, separators=(",", ":"))


logger = logging.getLogger("llmlingua-compressor")
handler = logging.StreamHandler()
handler.setFormatter(JSONFormatter())
logger.addHandler(handler)
logger.setLevel(os.getenv("LOG_LEVEL", "INFO").upper())
logger.propagate = False


class Handler(BaseHTTPRequestHandler):
    service: CompressionService
    server_version = "llmlingua-compressor"
    sys_version = ""

    def do_GET(self) -> None:  # noqa: N802
        if self.path in ("/healthz", "/readyz"):
            self._json(HTTPStatus.OK, {"status": "ok"})
            return
        self._json(HTTPStatus.NOT_FOUND, {"error": "not found"})

    def do_POST(self) -> None:  # noqa: N802
        started = time.monotonic()
        if self.path != "/v1/compress":
            self._json(HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > MAX_REQUEST_BYTES:
                raise ValueError("invalid request size")
            body = self.rfile.read(length)
            request = json.loads(body)
            texts = request.get("texts")
            if not isinstance(texts, list):
                raise ValueError("texts must be an array")
            target_ratio = float(request.get("target_ratio", 0.5))
            results = self.service.compress(texts, target_ratio)
            self._json(
                HTTPStatus.OK,
                {
                    "results": [result.__dict__ for result in results],
                    "model": os.getenv("MODEL_NAME", "/models/model"),
                },
            )
            logger.info(
                "compression completed",
                extra={
                    "path": self.path,
                    "status": HTTPStatus.OK,
                    "latency_ms": int((time.monotonic() - started) * 1000),
                    "batch_size": len(texts),
                },
            )
        except (ValueError, json.JSONDecodeError) as exc:
            self._json(HTTPStatus.BAD_REQUEST, {"error": str(exc)})
        except Exception as exc:  # inference failures must not crash the process
            logger.exception(
                "compression failed",
                extra={
                    "path": self.path,
                    "error": str(exc) or repr(exc),
                    "error_type": type(exc).__name__,
                },
            )
            self._json(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": "compression failed"})

    def log_message(self, _format: str, *_args: Any) -> None:
        return

    def _json(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)


def load_service() -> CompressionService:
    from llmlingua import PromptCompressor

    model_name = os.getenv(
        "MODEL_NAME", "/models/llmlingua-2-bert-base-multilingual-cased-meetingbank"
    )
    logger.info("loading compression model")
    engine = PromptCompressor(model_name=model_name, use_llmlingua2=True, device_map="cpu")
    logger.info("compression model ready")
    return CompressionService(engine)


def main() -> None:
    Handler.service = load_service()
    address = os.getenv("LISTEN_ADDRESS", "0.0.0.0")
    port = int(os.getenv("PORT", "8090"))
    server = ThreadingHTTPServer((address, port), Handler)
    logger.info(f"compressor listening on {address}:{port}")
    server.serve_forever()


if __name__ == "__main__":
    main()
