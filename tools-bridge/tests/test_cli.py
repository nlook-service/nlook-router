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
    # Agno may print ERROR/WARNING log lines to stdout.
    # Extract the JSON portion by finding the first { or [ and parsing from there.
    stdout = r.stdout
    for start_char in ("{", "["):
        idx = stdout.find(start_char)
        if idx >= 0:
            candidate = stdout[idx:]
            try:
                json.loads(candidate)
                stdout = candidate
                break
            except json.JSONDecodeError:
                pass
    return r.returncode, stdout, r.stderr


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


def test_run_search_web():
    code, out, err = _run("-q", "--run", "search_web", "--args", '{"query": "test"}')
    assert code == 0, (out, err)
    data = json.loads(out)
    assert data["status"] == "success", data


def test_run_save_and_read_file():
    # save
    code, out, err = _run(
        "-q", "--run", "save_file",
        "--args", '{"contents": "cli-test-ok", "file_name": "/tmp/nlook-cli-test.txt"}',
    )
    assert code == 0, (out, err)
    data = json.loads(out)
    assert data["status"] == "success", data

    # read
    code, out, err = _run(
        "-q", "--run", "read_file",
        "--args", '{"file_name": "/tmp/nlook-cli-test.txt"}',
    )
    assert code == 0, (out, err)
    data = json.loads(out)
    assert data["status"] == "success", data
    assert "cli-test-ok" in str(data.get("result", ""))


def test_list_includes_critical_tools():
    code, out, err = _run("--list")
    assert code == 0, (out, err)
    data = json.loads(out)
    names = {t["name"] for t in data}
    for required in ["add", "search_web", "read_file", "save_file", "run_python_code"]:
        assert required in names, f"{required} not in tool list"


if __name__ == "__main__":
    test_list_returns_json_array()
    test_run_add()
    test_run_search_web()
    test_run_save_and_read_file()
    test_list_includes_critical_tools()
    print("OK")
