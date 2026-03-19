#!/usr/bin/env python3
"""Tokenize text using HuggingFace tokenizer. Used by Go router for accurate token counting.

Usage:
    echo "Hello world" | python3 tokenize.py --model qwen3
    python3 tokenize.py --model qwen3 --text "Hello world"
    python3 tokenize.py --model qwen3 --count-only --text "Hello world"
"""
import argparse
import json
import sys
import os

# Suppress warnings
os.environ["TOKENIZERS_PARALLELISM"] = "false"

# Model name → HuggingFace tokenizer mapping
MODEL_TOKENIZER_MAP = {
    "qwen3": "Qwen/Qwen3-8B",
    "qwen2.5": "Qwen/Qwen2.5-7B-Instruct",
    "llama3": "meta-llama/Llama-3.1-8B-Instruct",
    "gemma": "google/gemma-2-9b-it",
    "mistral": "mistralai/Mistral-7B-Instruct-v0.3",
    "deepseek": "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B",
}

_tokenizer_cache = {}

def get_tokenizer(model_name: str):
    """Load and cache tokenizer."""
    if model_name in _tokenizer_cache:
        return _tokenizer_cache[model_name]

    # Find matching tokenizer
    model_key = model_name.lower().split(":")[0]
    hf_name = None
    for prefix, name in MODEL_TOKENIZER_MAP.items():
        if model_key.startswith(prefix):
            hf_name = name
            break

    if not hf_name:
        hf_name = MODEL_TOKENIZER_MAP.get("qwen3")  # Default

    try:
        from transformers import AutoTokenizer
        tokenizer = AutoTokenizer.from_pretrained(
            hf_name,
            trust_remote_code=True,
            cache_dir=os.path.expanduser("~/.nlook/tokenizer_cache"),
        )
        _tokenizer_cache[model_name] = tokenizer
        return tokenizer
    except Exception as e:
        print(json.dumps({"error": str(e)}), file=sys.stderr)
        return None


def count_tokens(text: str, model: str) -> dict:
    """Count tokens in text."""
    tokenizer = get_tokenizer(model)
    if tokenizer is None:
        # Fallback: estimate
        return {"count": len(text) // 3, "method": "estimate", "model": model}

    tokens = tokenizer.encode(text)
    return {
        "count": len(tokens),
        "method": "exact",
        "model": model,
        "vocab_size": tokenizer.vocab_size,
        "max_length": getattr(tokenizer, "model_max_length", 0),
    }


def main():
    parser = argparse.ArgumentParser(description="Tokenize text")
    parser.add_argument("--model", default="qwen3", help="Model name")
    parser.add_argument("--text", default=None, help="Text to tokenize (or stdin)")
    parser.add_argument("--count-only", action="store_true", help="Output only count")
    args = parser.parse_args()

    if args.text:
        text = args.text
    else:
        text = sys.stdin.read()

    result = count_tokens(text, args.model)

    if args.count_only:
        print(result["count"])
    else:
        print(json.dumps(result))


if __name__ == "__main__":
    main()
