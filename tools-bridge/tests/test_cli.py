"""Minimal tests for tool_bridge CLI (list and run).
Run from router repo root:
  python -m pytest tools-bridge/tests -v
  or: python3 tools-bridge/tests/test_cli.py
"""

import json
import subprocess
import sys
from pathlib import Path

# Add tool_bridge to path when running from router repo root
ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))


def _run(*args):
    r = subprocess.run(
        [sys.executable, "-m", "tool_bridge"] + list(args),
        capture_output=True,
        text=True,
        cwd=ROOT,
    )
    return r.returncode, r.stdout, r.stderr


def test_list_returns_json_array():
    code, out, err = _run("--list")
    assert code == 0, (out, err)
    data = json.loads(out)
    assert isinstance(data, list)
    assert len(data) >= 1
    names = [t["name"] for t in data]
    assert "add" in names
    assert "subtract" in names


def test_run_add():
    code, out, err = _run("--run", "add", "--args", '{"a": 1, "b": 2}')
    assert code == 0, (out, err)
    data = json.loads(out)
    assert data["status"] == "success"
    assert data["error"] is None
    # result is JSON string from calculator
    result = json.loads(data["result"]) if isinstance(data["result"], str) else data["result"]
    assert result.get("operation") == "addition"
    assert result.get("result") == 3


if __name__ == "__main__":
    test_list_returns_json_array()
    test_run_add()
    print("OK")
