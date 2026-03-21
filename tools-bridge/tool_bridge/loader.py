"""Load Agno toolkits and merge into a single functions dict. No Agno source modified.

모든 툴킷을 시도하고, import/초기화 실패한 항목만 제외. 에러 메시지는 stderr로 출력되어
필요 시 환경 설정(환경 변수·패키지 설치) 가이드를 참고하면 됩니다.
"""

from pathlib import Path
from typing import Any, Dict, Optional

# Agno public API only
from agno.tools import Toolkit
from agno.tools.function import Function


# 모든 Agno 툴킷 (module_path, class_name). 실패한 항목은 로드에서 제외되며 에러는 stderr에 출력.
# 환경 설정 가이드: tools-bridge/README.md 의 "환경 설정 가이드" 참고.
AGNO_TOOLKITS: list[tuple[str, str]] = [
    ("agentql", "AgentQLTools"),
    ("airflow", "AirflowTools"),
    ("api", "CustomApiTools"),
    ("apify", "ApifyTools"),
    ("arxiv", "ArxivTools"),
    ("aws_lambda", "AWSLambdaTools"),
    ("aws_ses", "AWSSESTool"),
    ("baidusearch", "BaiduSearchTools"),
    ("bitbucket", "BitbucketTools"),
    ("brandfetch", "BrandfetchTools"),
    ("bravesearch", "BraveSearchTools"),
    ("brightdata", "BrightDataTools"),
    ("browserbase", "BrowserbaseTools"),
    ("calcom", "CalComTools"),
    ("calculator", "CalculatorTools"),
    ("cartesia", "CartesiaTools"),
    ("clickup", "ClickUpTools"),
    ("coding", "CodingTools"),
    ("confluence", "ConfluenceTools"),
    ("crawl4ai", "Crawl4aiTools"),
    ("csv_toolkit", "CsvTools"),
    ("dalle", "DalleTools"),
    ("daytona", "DaytonaTools"),
    ("desi_vocal", "DesiVocalTools"),
    ("discord", "DiscordTools"),
    ("docker", "DockerTools"),
    ("duckdb", "DuckDbTools"),
    ("duckduckgo", "DuckDuckGoTools"),
    ("e2b", "E2BTools"),
    ("eleven_labs", "ElevenLabsTools"),
    ("email", "EmailTools"),
    ("evm", "EvmTools"),
    ("exa", "ExaTools"),
    ("fal", "FalTools"),
    ("file", "FileTools"),
    ("file_generation", "FileGenerationTools"),
    ("financial_datasets", "FinancialDatasetsTools"),
    ("firecrawl", "FirecrawlTools"),
    ("giphy", "GiphyTools"),
    ("github", "GithubTools"),
    ("gitlab", "GitlabTools"),
    ("google.bigquery", "GoogleBigQueryTools"),
    ("google.calendar", "GoogleCalendarTools"),
    ("google.drive", "GoogleDriveTools"),
    ("google.gmail", "GmailTools"),
    ("google.maps", "GoogleMapTools"),
    ("google.sheets", "GoogleSheetsTools"),
    ("hackernews", "HackerNewsTools"),
    ("jina", "JinaReaderTools"),
    ("jira", "JiraTools"),
    ("knowledge", "KnowledgeTools"),
    ("linear", "LinearTools"),
    ("linkup", "LinkupTools"),
    ("local_file_system", "LocalFileSystemTools"),
    ("lumalab", "LumaLabTools"),
    ("mcp.mcp", "MCPTools"),
    ("mcp.multi_mcp", "MultiMCPTools"),
    ("mem0", "Mem0Tools"),
    ("memory", "MemoryTools"),
    ("mlx_transcribe", "MLXTranscribeTools"),
    ("models_labs", "ModelsLabTools"),
    ("models.azure_openai", "AzureOpenAITools"),
    ("models.gemini", "GeminiTools"),
    ("models.groq", "GroqTools"),
    ("models.morph", "MorphTools"),
    ("models.nebius", "NebiusTools"),
    ("moviepy_video", "MoviePyVideoTools"),
    ("nano_banana", "NanoBananaTools"),
    ("neo4j", "Neo4jTools"),
    ("newspaper", "NewspaperTools"),
    ("newspaper4k", "Newspaper4kTools"),
    ("notion", "NotionTools"),
    ("openai", "OpenAITools"),
    ("openbb", "OpenBBTools"),
    ("opencv", "OpenCVTools"),
    ("openweather", "OpenWeatherTools"),
    ("oxylabs", "OxylabsTools"),
    ("pandas", "PandasTools"),
    ("parallel", "ParallelTools"),
    ("postgres", "PostgresTools"),
    ("pubmed", "PubmedTools"),
    ("python", "PythonTools"),
    ("reasoning", "ReasoningTools"),
    ("reddit", "RedditTools"),
    ("redshift", "RedshiftTools"),
    ("replicate", "ReplicateTools"),
    ("resend", "ResendTools"),
    ("scrapegraph", "ScrapeGraphTools"),
    ("seltz", "SeltzTools"),
    ("serpapi", "SerpApiTools"),
    ("serper", "SerperTools"),
    ("shell", "ShellTools"),
    ("shopify", "ShopifyTools"),
    ("slack", "SlackTools"),
    ("sleep", "SleepTools"),
    ("spider", "SpiderTools"),
    ("spotify", "SpotifyTools"),
    ("sql", "SQLTools"),
    ("tavily", "TavilyTools"),
    ("telegram", "TelegramTools"),
    ("todoist", "TodoistTools"),
    ("trafilatura", "TrafilaturaTools"),
    ("trello", "TrelloTools"),
    ("twilio", "TwilioTools"),
    ("unsplash", "UnsplashTools"),
    ("user_control_flow", "UserControlFlowTools"),
    ("user_feedback", "UserFeedbackTools"),
    ("valyu", "ValyuTools"),
    ("visualization", "VisualizationTools"),
    ("webbrowser", "WebBrowserTools"),
    ("webex", "WebexTools"),
    ("websearch", "WebSearchTools"),
    ("website", "WebsiteTools"),
    ("webtools", "WebTools"),
    ("whatsapp", "WhatsAppTools"),
    ("wikipedia", "WikipediaTools"),
    ("workflow", "WorkflowTools"),
    ("x", "XTools"),
    ("yfinance", "YFinanceTools"),
    ("youtube", "YouTubeTools"),
    ("zendesk", "ZendeskTools"),
    ("zep", "ZepAsyncTools"),
    ("zep", "ZepTools"),
    ("zoom", "ZoomTools"),
]


# 우선순위 도구: 마지막에 로드되어 이름 충돌 시 이 구현이 최종 사용됨.
# FileTools의 read_file/list_files가 PythonTools보다 유연 (경로 제한 없음).
PRIORITY_TOOLKITS: list[tuple[str, str]] = [
    ("file", "FileTools"),
    ("shell", "ShellTools"),
    ("calculator", "CalculatorTools"),
    ("sleep", "SleepTools"),
]

_PRIORITY_SET = {(m, c) for m, c in PRIORITY_TOOLKITS}


# 툴킷별 커스텀 초기화 인자 (기본값이 제한적인 경우)
_TOOLKIT_INIT_KWARGS: Dict[str, dict] = {
    "FileTools": {"base_dir": Path("/")},
    "SerperTools": {"location": "kr", "language": "ko"},
}


def _load_one_toolkit(module_path: str, class_name: str) -> "Optional[Toolkit]":
    """Import and instantiate one toolkit. Returns None on any failure."""
    import sys as _sys
    try:
        mod = __import__(f"agno.tools.{module_path}", fromlist=[class_name])
        cls = getattr(mod, class_name)
        kwargs = _TOOLKIT_INIT_KWARGS.get(class_name, {})
        return cls(**kwargs)
    except ImportError as e:
        print(f"[tools] skip {class_name}: missing package ({e})", file=_sys.stderr)
        return None
    except Exception as e:
        if "API" in str(e).upper() or "KEY" in str(e).upper():
            print(f"[tools] skip {class_name}: API key not configured", file=_sys.stderr)
        return None


def load_default_toolkits() -> Dict[str, Function]:
    """Load all Agno toolkits. Priority toolkits load last to win name conflicts."""
    toolkits: list[Toolkit] = []
    # 1) Load non-priority toolkits first
    for mod_path, class_name in AGNO_TOOLKITS:
        if (mod_path, class_name) in _PRIORITY_SET:
            continue
        tk = _load_one_toolkit(mod_path, class_name)
        if tk is not None:
            toolkits.append(tk)
    # 2) Load priority toolkits last (wins name conflicts)
    for mod_path, class_name in PRIORITY_TOOLKITS:
        tk = _load_one_toolkit(mod_path, class_name)
        if tk is not None:
            toolkits.append(tk)
    return merge_toolkits(toolkits)


def merge_toolkits(toolkits: list[Toolkit]) -> Dict[str, Function]:
    """Merge multiple toolkits into one name -> Function dict. Later overrides earlier on name clash."""
    out: Dict[str, Function] = {}
    for tk in toolkits:
        for name, fn in tk.get_functions().items():
            out[name] = fn
    return out


def tools_to_list(functions: Dict[str, Function]) -> list[Dict[str, Any]]:
    """Serialize functions to list of dicts (name, description, parameters) for JSON."""
    return [fn.to_dict() for fn in functions.values()]
