"""CLI/Doclib 连接 4.1 服务时按能力检查协议，避免旧 V1 服务假就绪。"""

from __future__ import annotations

import asyncio
from typing import Any

import httpx
import pytest

from doclib_test_utils import create_health_check
from mineru.doclib.parse_server_state import ParseServerHealth, ProbeResult, ProbeState
from mineru.doclib.services.parse_errors import ParseFailure
from mineru.doclib.services.parse_policy import resolve_default_tier


@pytest.mark.parametrize(
    "features,compatible",
    [
        ({"task_output_formats": ["default", "docx"], "sources": ["upload", "url"]}, True),
        ({"output_formats": ["markdown", "zip"], "sources": ["upload", "local"]}, False),
        ({"task_output_formats": ["default"], "sources": ["url"]}, False),
        ({"task_output_formats": ["docx"], "sources": ["upload"]}, False),
        ({}, False),
        (None, False),
    ],
)
def test_probe_checks_tasks_contract_before_tiers(
    monkeypatch: pytest.MonkeyPatch, features: dict[str, Any] | None, compatible: bool
) -> None:
    """保留代理前缀和鉴权；旧服务不查询档位，错误直接指导 CLI 用户升级。"""
    paths: list[str] = []

    def handle(request: httpx.Request) -> httpx.Response:
        paths.append(request.url.path)
        assert request.headers["authorization"] == "Bearer test-key"
        if request.url.path.endswith("/health"):
            return httpx.Response(200, json={"features": features})
        assert request.url.path == "/gateway/v1/tiers"
        return httpx.Response(200, json={"data": [{"id": "standard"}]})

    client_type = httpx.AsyncClient
    monkeypatch.setattr(
        "mineru.doclib.background.parse_server_health.httpx.AsyncClient",
        lambda **kwargs: client_type(transport=httpx.MockTransport(handle), **kwargs),
    )
    checker = create_health_check(None, interval_sec=1, probe_timeout_sec=2, startup_grace_sec=3, stop_timeout_sec=4)
    result = asyncio.run(checker._probe("https://api.test/gateway/", api_key="test-key"))
    assert result.healthy is compatible
    expected_paths = ["/gateway/v1/health", "/gateway/v1/tiers"] if compatible else ["/gateway/v1/health"]
    assert paths == expected_paths
    if compatible:
        assert result == ProbeResult(healthy=True, tiers=["standard"])
    else:
        assert "Upgrade the API server, Router and client together" in (result.error_msg or "")
        health = ParseServerHealth(remote=ProbeState(probe=result))
        with pytest.raises(ParseFailure, match="Upgrade the API server, Router and client together"):
            resolve_default_tier(health=health, remote=True)
