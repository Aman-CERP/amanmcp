#!/usr/bin/env python3
"""
Download MLX embedding model for AmanMCP.

This script pre-downloads the embedding model so the server starts instantly.
Progress bar is provided by HuggingFace Hub's built-in downloader.

Usage:
    python download_model.py [model_alias]

Model aliases:
    small (default) - Qwen3-Embedding-0.6B (~1.5GB)
    medium         - Qwen3-Embedding-4B (~8GB)
    large          - Qwen3-Embedding-8B (~16GB)
"""

import os
import sys
from pathlib import Path

# Set up model cache directory before importing mlx_lm
MODELS_DIR = Path.home() / ".amanmcp" / "models" / "mlx"
MODELS_DIR.mkdir(parents=True, exist_ok=True)
os.environ["HF_HOME"] = str(MODELS_DIR)
os.environ["HUGGINGFACE_HUB_CACHE"] = str(MODELS_DIR / "hub")

# Model configurations
MODELS = {
    "small": {
        "name": "mlx-community/Qwen3-Embedding-0.6B-4bit-DWQ",
        "size": "~1.5GB",
        "dims": 1024,
    },
    "medium": {
        "name": "mlx-community/Qwen3-Embedding-4B-4bit-DWQ",
        "size": "~8GB",
        "dims": 2560,
    },
    "large": {
        "name": "mlx-community/Qwen3-Embedding-8B-4bit-DWQ",
        "size": "~16GB",
        "dims": 4096,
    },
}

def main():
    # Parse model alias from args
    model_alias = sys.argv[1] if len(sys.argv) > 1 else "small"

    if model_alias not in MODELS:
        print(f"Unknown model: {model_alias}")
        print(f"Available: {', '.join(MODELS.keys())}")
        sys.exit(1)

    model_info = MODELS[model_alias]
    model_name = model_info["name"]

    print()
    print("=" * 60)
    print("  AmanMCP MLX Model Downloader")
    print("=" * 60)
    print()
    print(f"  Model:      {model_alias} ({model_info['dims']} dimensions)")
    print(f"  Size:       {model_info['size']}")
    print(f"  Location:   {MODELS_DIR}")
    print()
    print("  Downloading from HuggingFace Hub...")
    print("  (Progress bar will appear below)")
    print()
    print("-" * 60)

    try:
        # Import after setting env vars
        from mlx_lm import load

        # This triggers the download with progress bar
        model, tokenizer = load(model_name)

        # Quick validation
        test_tokens = tokenizer.encode("test")

        print("-" * 60)
        print()
        print("  Download complete!")
        print()
        print(f"  Model ready at: {MODELS_DIR}")
        print()
        print("  To start the server:")
        print("    cd mlx-server && .venv/bin/python server.py")
        print()
        print("  Or with amanmcp:")
        print("    amanmcp index --backend=mlx .")
        print()
        print("=" * 60)

    except KeyboardInterrupt:
        print("\n\nDownload cancelled.")
        sys.exit(1)
    except Exception as e:
        print(f"\nDownload failed: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
