#!/usr/bin/env python3
"""Verify critical tools work end-to-end. Exit 0 if all pass, 1 if any fail.

Usage:
    python3 verify_tools.py          # run all critical tools
    python3 verify_tools.py --json   # output JSON results
"""
import io
import json
import os
import sys
from typing import Any, Callable, Dict, List, Optional, Tuple

# Suppress load noise
_real_stdout, _real_stderr = sys.stdout, sys.stderr
sys.stdout = open(os.devnull, "w")
sys.stderr = open(os.devnull, "w")
try:
    from tool_bridge.loader import load_default_toolkits
    from agno.utils.functions import get_function_call
finally:
    sys.stdout, sys.stderr = _real_stdout, _real_stderr


# (name, args, validator: result_string -> bool)
CRITICAL_TOOLS: List[Tuple[str, dict, Callable[[Any], bool]]] = [
    # Calculator
    ("add", {"a": 1, "b": 2}, lambda r: "3" in str(r)),
    ("subtract", {"a": 5, "b": 2}, lambda r: "3" in str(r)),
    ("multiply", {"a": 2, "b": 3}, lambda r: "6" in str(r)),
    # Web Search
    ("search_web", {"query": "python programming"}, lambda r: r is not None and len(str(r)) > 10),
    ("search_news", {"query": "technology"}, lambda r: r is not None),
    # File I/O
    ("save_file", {"contents": "verify-test-ok", "file_name": "/tmp/nlook-verify.txt"}, lambda r: True),
    ("read_file", {"file_name": "/tmp/nlook-verify.txt"}, lambda r: "verify-test-ok" in str(r) or "Error" not in str(r)),
    ("list_files", {}, lambda r: r is not None),
    # Code Execution
    ("run_python_code", {"code": "print(42)"}, lambda r: "42" in str(r) or "success" in str(r).lower()),
    # Shell
    ("run_shell", {"command": "echo hello"}, lambda r: "hello" in str(r)),
    # Web
    ("read_url", {"url": "https://httpbin.org/get"}, lambda r: "httpbin" in str(r).lower() or "origin" in str(r)),
    # HackerNews
    ("get_top_hackernews_stories", {"num_stories": 1}, lambda r: r is not None),
]


def run_verification(functions: Dict, output_json: bool = False) -> bool:
    results: List[Dict[str, Any]] = []
    all_pass = True

    for name, args, validator in CRITICAL_TOOLS:
        # Capture stdout during execution
        sys.stdout = io.StringIO()
        sys.stderr = io.StringIO()
        try:
            fc = get_function_call(name=name, arguments=json.dumps(args), functions=functions)
            if fc is None:
                results.append({"name": name, "status": "skip", "error": "not loaded"})
                continue
            if fc.error:
                results.append({"name": name, "status": "failure", "error": fc.error})
                all_pass = False
                continue
            result = fc.execute()
            if result.status == "success" and validator(result.result):
                results.append({"name": name, "status": "success", "error": None})
            else:
                err = result.error or f"validator failed on: {str(result.result)[:100]}"
                results.append({"name": name, "status": "failure", "error": err})
                all_pass = False
        except Exception as e:
            results.append({"name": name, "status": "failure", "error": str(e)})
            all_pass = False
        finally:
            sys.stdout = _real_stdout
            sys.stderr = _real_stderr

    if output_json:
        print(json.dumps(results, indent=2, ensure_ascii=False))
    else:
        passed = sum(1 for r in results if r["status"] == "success")
        skipped = sum(1 for r in results if r["status"] == "skip")
        failed = sum(1 for r in results if r["status"] == "failure")
        print(f"Critical Tools: {passed} passed, {failed} failed, {skipped} skipped / {len(results)} total")
        print()
        for r in results:
            icon = {"success": "✅", "failure": "❌", "skip": "⏭️"}[r["status"]]
            err = f" — {r['error']}" if r.get("error") else ""
            print(f"  {icon} {r['name']}{err}")
        print()
        if all_pass:
            print("All critical tools passed.")
        else:
            print("Some critical tools failed!")

    return all_pass


def main() -> None:
    output_json = "--json" in sys.argv
    functions = load_default_toolkits()
    ok = run_verification(functions, output_json=output_json)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
