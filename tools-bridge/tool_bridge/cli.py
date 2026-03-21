"""CLI: --list (stdout JSON tools), --run NAME --args JSON (stdout JSON result)."""

import argparse
import io
import json
import logging
import os
import sys
from typing import Any, Dict

from agno.tools.function import Function
from agno.utils.functions import get_function_call

from .loader import load_default_toolkits, tools_to_list

# 툴별 최소 인자 (--test-all 시 사용). 없으면 {} 로 호출.
SAFE_TEST_ARGS: Dict[str, dict] = {
    # Calculator
    "add": {"a": 1, "b": 2},
    "subtract": {"a": 5, "b": 2},
    "multiply": {"a": 2, "b": 3},
    "divide": {"a": 10, "b": 2},
    "exponentiate": {"a": 2, "b": 3},
    "factorial": {"n": 5},
    "is_prime": {"n": 7},
    "square_root": {"n": 4},
    "sleep": {"seconds": 0},
    # Web Search (SerperTools param: query only)
    "search_web": {"query": "test"},
    "search_news": {"query": "test"},
    # File (PythonTools overrides FileTools for read_file/list_files)
    "save_file": {"contents": "nlook-test", "file_name": "/tmp/nlook-tools-test.txt"},
    "read_file": {"file_name": "/tmp/nlook-tools-test.txt"},
    "list_files": {},
    "write_file": {"content": "nlook-test", "filename": "nlook-tools-test2.txt", "directory": "/tmp"},
    "search_files": {"pattern": "*.txt"},
    "search_content": {"query": "nlook"},
    # Code
    "run_python_code": {"code": "print(1+1)"},
    # Shell (ShellTools uses args list, CodingTools uses command string)
    "run_shell": {"command": "echo ok"},
    "run_shell_command": {"args": ["echo", "ok"]},
    # HackerNews
    "get_top_hackernews_stories": {"num_stories": 1},
    # Web
    "read_url": {"url": "https://httpbin.org/get"},
    "scrape_webpage": {"url": "https://httpbin.org/get"},
}


def _send_agno_logs_to_stderr() -> None:
    """Point all Agno logger handlers to stderr so stdout stays JSON-only."""
    for name in ("agno", "agno.agent", "agno.team", "agno.workflow"):
        log = logging.getLogger(name)
        for h in getattr(log, "handlers", []):
            if hasattr(h, "stream"):
                h.stream = sys.stderr


def _list_cmd(functions: Dict[str, Function]) -> None:
    """Output JSON array of tool meta to stdout."""
    lst = tools_to_list(functions)
    json.dump(lst, sys.stdout, indent=0, ensure_ascii=False)
    sys.stdout.write("\n")


def _run_cmd(functions: Dict[str, Function], name: str, args_json: str) -> None:
    """Run one tool, output JSON {status, result, error} to stdout."""
    try:
        args_str = args_json.strip() if args_json else "{}"
    except Exception:
        args_str = "{}"

    fc = get_function_call(name=name, arguments=args_str, functions=functions)
    if fc is None:
        out = {"status": "failure", "result": None, "error": f"Tool not found: {name}"}
        json.dump(out, sys.stdout, ensure_ascii=False)
        sys.stdout.write("\n")
        return

    if fc.error:
        out = {"status": "failure", "result": None, "error": fc.error}
        json.dump(out, sys.stdout, ensure_ascii=False)
        sys.stdout.write("\n")
        return

    result = fc.execute()
    if result.status == "success":
        out = {"status": "success", "result": result.result, "error": None}
    else:
        out = {"status": "failure", "result": result.result, "error": result.error}

    def _serialize(obj: Any) -> Any:
        if hasattr(obj, "model_dump"):
            return obj.model_dump()
        if hasattr(obj, "dict"):
            return obj.dict()
        raise TypeError(type(obj))

    try:
        json.dump(out, sys.stdout, default=_serialize, ensure_ascii=False)
    except TypeError:
        out["result"] = str(result.result)
        json.dump(out, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


def _test_all_cmd(functions: Dict[str, Function]) -> None:
    """Run each tool with SAFE_TEST_ARGS or {} and output JSON array of {name, status, error?}."""
    import json as _json
    # execute() 중 Agno가 stdout에 로그할 수 있으므로, 툴 실행 시에는 stdout을 버퍼로 돌려 JSON만 실제 stdout에 출력
    real_stdout = sys.stdout
    real_stderr = sys.stderr
    results: list[Dict[str, Any]] = []
    for name in sorted(functions.keys()):
        sys.stdout = io.StringIO()
        sys.stderr = io.StringIO()
        try:
            args = SAFE_TEST_ARGS.get(name, {})
            args_str = _json.dumps(args)
            fc = get_function_call(name=name, arguments=args_str, functions=functions)
            if fc is None:
                results.append({"name": name, "status": "failure", "error": "Tool not found"})
                continue
            if fc.error:
                results.append({"name": name, "status": "failure", "error": fc.error})
                continue
            result = fc.execute()
            if result.status == "success":
                results.append({"name": name, "status": "success", "error": None})
            else:
                results.append({"name": name, "status": "failure", "error": result.error})
        except Exception as e:
            results.append({"name": name, "status": "failure", "error": str(e)})
        finally:
            sys.stdout = real_stdout
            sys.stderr = real_stderr
    json.dump(results, sys.stdout, indent=0, ensure_ascii=False)
    sys.stdout.write("\n")


def main() -> None:
    parser = argparse.ArgumentParser(description="Nlook tools bridge: list or run Agno tools")
    parser.add_argument("--list", action="store_true", help="Output JSON array of available tools")
    parser.add_argument("--test-all", action="store_true", help="Run every tool once (safe args or {}), output JSON array of {name, status, error?}")
    parser.add_argument("--run", metavar="NAME", help="Run tool by name")
    parser.add_argument("--args", default="{}", help="JSON object of arguments for --run")
    parser.add_argument("-q", "--quiet", action="store_true", help="Suppress ERROR/WARNING; only print JSON to stdout")
    args = parser.parse_args()

    if args.quiet:
        sys.stderr = open(os.devnull, "w")

    # 툴킷 로드 시에만 로그/에러 숨김: 사용 중인 툴(add 등)과 무관한 CalCom/Discord/Zoom 등 메시지가 안 나옴
    _real_stdout = sys.stdout
    _real_stderr = sys.stderr
    sys.stdout = open(os.devnull, "w")
    sys.stderr = open(os.devnull, "w")
    try:
        functions = load_default_toolkits()
    finally:
        sys.stdout = _real_stdout
        sys.stderr = _real_stderr
    _send_agno_logs_to_stderr()

    if args.list:
        _list_cmd(functions)
        return

    if args.test_all:
        _test_all_cmd(functions)
        return

    if args.run:
        _run_cmd(functions, args.run, args.args)
        return

    parser.print_help()
    sys.exit(1)
