#!/usr/bin/env python3
"""
Qwen3 Embedding Server using MLX on Apple Silicon

A high-performance text embedding server optimized for Apple Silicon Macs,
providing REST API access to Qwen3 embedding models via the MLX framework.

This is a modified version bundled with AmanMCP.
Original source: https://github.com/jakedahn/qwen3-embeddings-mlx
License: MIT (see LICENSE file)

Modifications for AmanMCP:
- Default port changed to 9659 (avoid conflicts with common port 8000)
- Model cache location configurable via AMANMCP_MLX_MODELS_DIR
"""

import gc
import os
import sys
import time
import json
import asyncio
import logging
from datetime import datetime, timezone
from logging.handlers import RotatingFileHandler
from typing import List, Optional, Dict, Any, Tuple
from functools import lru_cache
from contextlib import asynccontextmanager
from dataclasses import dataclass
from enum import Enum
from pathlib import Path

import numpy as np
import mlx
import mlx.core as mx
from mlx_lm import load
from fastapi import FastAPI, HTTPException, Request, status
from fastapi.responses import JSONResponse
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field, ConfigDict, field_validator
import uvicorn

# Constants - AmanMCP modifications
# Changed: Use 0.6B as default - 94% quality retention at 5x less memory
# See: .aman-pm/backlog/tasks/TASK-MEM1.md for rationale
# 8B model (15-35 GB) causes system freezes; 0.6B (~3 GB) is stable
DEFAULT_MODEL = "mlx-community/Qwen3-Embedding-0.6B-4bit-DWQ"
DEFAULT_PORT = 9659  # Changed: Avoid conflicts with common port 8000

# AmanMCP model storage directory
def get_models_dir() -> str:
    """Get the MLX models directory, defaulting to ~/.amanmcp/models/mlx/"""
    custom_dir = os.getenv("AMANMCP_MLX_MODELS_DIR")
    if custom_dir:
        return custom_dir
    return str(Path.home() / ".amanmcp" / "models" / "mlx")

# Available embedding models configuration
AVAILABLE_MODELS = {
    "mlx-community/Qwen3-Embedding-0.6B-4bit-DWQ": {
        "alias": ["small", "0.6b", "default"],  # default changed from 8B to 0.6B
        "embedding_dim": 1024,
        "description": "Small 0.6B parameter model, fast and efficient (default)"
    },
    "mlx-community/Qwen3-Embedding-4B-4bit-DWQ": {
        "alias": ["medium", "4b"],
        "embedding_dim": 2560,
        "description": "Medium 4B parameter model, balanced performance"
    },
    "mlx-community/Qwen3-Embedding-8B-4bit-DWQ": {
        "alias": ["large", "8b"],
        "embedding_dim": 4096,
        "description": "Large 8B parameter model, higher quality (requires 32GB+ RAM)"
    },
    # EmbeddingGemma - SPIKE-003: 50% smaller than Qwen3-0.6B with MRL support
    "mlx-community/embeddinggemma-300m-4bit": {
        "alias": ["embeddinggemma", "gemma", "gemma-300m"],
        "embedding_dim": 768,
        "description": "Google EmbeddingGemma 308M, 50% smaller than Qwen3-0.6B, MRL dimension support (768/512/256/128)"
    }
}

# Available reranker models configuration (FEAT-RR1)
# Qwen3-Reranker uses yes/no token logits for relevance scoring
# NOTE: Using official Qwen models - mlx-community quantized versions not yet available
AVAILABLE_RERANKER_MODELS = {
    "Qwen/Qwen3-Reranker-0.6B": {
        "alias": ["reranker-small", "reranker-0.6b", "reranker-default"],
        "max_length": 8192,
        "description": "Small 0.6B reranker, fast cross-encoder scoring (~1.5GB)"
    },
    "Qwen/Qwen3-Reranker-4B": {
        "alias": ["reranker-medium", "reranker-4b"],
        "max_length": 8192,
        "description": "Medium 4B reranker, balanced quality (~8GB)"
    },
    "Qwen/Qwen3-Reranker-8B": {
        "alias": ["reranker-large", "reranker-8b"],
        "max_length": 8192,
        "description": "Large 8B reranker, highest quality (~16GB)"
    }
}

# Default reranker model (matches embedding model size)
DEFAULT_RERANKER_MODEL = "Qwen/Qwen3-Reranker-0.6B"

# Build alias mapping for embedding models
MODEL_ALIASES = {}
for model_name, config in AVAILABLE_MODELS.items():
    for alias in config.get("alias", []):
        MODEL_ALIASES[alias.lower()] = model_name

# Build alias mapping for reranker models (FEAT-RR1)
RERANKER_ALIASES = {}
for model_name, config in AVAILABLE_RERANKER_MODELS.items():
    for alias in config.get("alias", []):
        RERANKER_ALIASES[alias.lower()] = model_name

MIN_BATCH_SIZE = 1
DEFAULT_MAX_BATCH = 1024
DEFAULT_MAX_LENGTH = 8192
DEFAULT_HOST = "0.0.0.0"

# Memory management settings (FEAT-MEM1)
# Models are loaded lazily on first request, not at startup
# After IDLE_TTL_SECONDS of no requests, models are unloaded to free memory
DEFAULT_IDLE_TTL_SECONDS = 300  # 5 minutes
DEFAULT_LAZY_LOAD = True  # Load on first request, not at startup

# Configure logging
def get_log_dir() -> Path:
    """Get the AmanMCP logs directory, defaulting to ~/.amanmcp/logs/"""
    custom_dir = os.getenv("AMANMCP_LOG_DIR")
    if custom_dir:
        return Path(custom_dir)
    return Path.home() / ".amanmcp" / "logs"


class JSONFormatter(logging.Formatter):
    """JSON formatter matching Go's slog output format for unified log viewing."""

    def format(self, record: logging.LogRecord) -> str:
        # Build base log entry matching Go's slog JSON format
        log_entry = {
            "time": datetime.now(timezone.utc).astimezone().isoformat(),
            "level": record.levelname,
            "msg": record.getMessage(),
            "source": "mlx",  # Label to distinguish from Go logs
        }

        # Add extra fields if present (for structured logging)
        if hasattr(record, 'extra') and record.extra:
            log_entry.update(record.extra)

        # Add exception info if present
        if record.exc_info:
            log_entry["error"] = self.formatException(record.exc_info)

        return json.dumps(log_entry, default=str)


class ConsoleFormatter(logging.Formatter):
    """Human-readable console formatter with colors."""

    COLORS = {
        'DEBUG': '\033[90m',    # Gray
        'INFO': '\033[32m',     # Green
        'WARNING': '\033[33m',  # Yellow
        'ERROR': '\033[31m',    # Red
        'CRITICAL': '\033[35m', # Magenta
    }
    RESET = '\033[0m'

    def format(self, record: logging.LogRecord) -> str:
        color = self.COLORS.get(record.levelname, '')
        timestamp = datetime.now().strftime('%H:%M:%S.%f')[:-3]
        level = f"{color}{record.levelname:<5}{self.RESET}"
        return f"{timestamp} {level} [mlx] {record.getMessage()}"


class HealthCheckFilter(logging.Filter):
    """Filter to exclude health check logs from file output (reduces noise)."""

    # Patterns that indicate health check related logs
    HEALTH_PATTERNS = [
        '/health',
        'health check',
        'health_check',
    ]

    def filter(self, record: logging.LogRecord) -> bool:
        """Return False to exclude the record, True to include it."""
        msg = record.getMessage().lower()
        for pattern in self.HEALTH_PATTERNS:
            if pattern in msg:
                return False  # Exclude this record
        return True  # Include this record


def setup_logging(level: str = "INFO") -> logging.Logger:
    """
    Configure application logging with both file and console output.

    File output: JSON format to ~/.amanmcp/logs/mlx-server.log (rotating)
    Console output: Human-readable colored format to stdout
    """
    log_dir = get_log_dir()
    log_dir.mkdir(parents=True, exist_ok=True)
    log_file = log_dir / "mlx-server.log"

    # Create logger
    logger = logging.getLogger(__name__)
    logger.setLevel(getattr(logging, level.upper()))

    # Clear any existing handlers
    logger.handlers.clear()

    # File handler: JSON format with rotation (10MB, 5 backups = 50MB max)
    file_handler = RotatingFileHandler(
        log_file,
        maxBytes=10 * 1024 * 1024,  # 10MB
        backupCount=5,
        encoding='utf-8'
    )
    file_handler.setFormatter(JSONFormatter())
    file_handler.setLevel(logging.DEBUG)  # Log everything to file
    file_handler.addFilter(HealthCheckFilter())  # Exclude health check noise
    logger.addHandler(file_handler)

    # Console handler: Human-readable colored format
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setFormatter(ConsoleFormatter())
    console_handler.setLevel(getattr(logging, level.upper()))
    logger.addHandler(console_handler)

    # Prevent propagation to root logger
    logger.propagate = False

    # Log startup message
    logger.info(f"Logging initialized: file={log_file}, level={level}")

    return logger


# Initialize logger
logger = setup_logging(os.getenv("LOG_LEVEL", "INFO"))

# Configuration
@dataclass
class ServerConfig:
    """Server configuration"""
    model_name: str = os.getenv("MODEL_NAME", DEFAULT_MODEL)
    max_batch_size: int = int(os.getenv("MAX_BATCH_SIZE", str(DEFAULT_MAX_BATCH)))
    max_text_length: int = int(os.getenv("MAX_TEXT_LENGTH", str(DEFAULT_MAX_LENGTH)))
    port: int = int(os.getenv("PORT", str(DEFAULT_PORT)))
    host: str = os.getenv("HOST", DEFAULT_HOST)
    enable_cors: bool = os.getenv("ENABLE_CORS", "true").lower() == "true"
    cors_origins: List[str] = None
    models_dir: str = get_models_dir()

    # Memory management settings (FEAT-MEM1)
    lazy_load: bool = os.getenv("LAZY_LOAD", str(DEFAULT_LAZY_LOAD)).lower() == "true"
    idle_ttl_seconds: int = int(os.getenv("IDLE_TTL_SECONDS", str(DEFAULT_IDLE_TTL_SECONDS)))

    def __post_init__(self):
        """Validate configuration"""
        if self.cors_origins is None:
            self.cors_origins = os.getenv("CORS_ORIGINS", "*").split(",")
        if self.max_batch_size < MIN_BATCH_SIZE:
            raise ValueError(f"max_batch_size must be at least {MIN_BATCH_SIZE}")
        if self.max_text_length < 1:
            raise ValueError("max_text_length must be positive")
        if self.port < 1 or self.port > 65535:
            raise ValueError("port must be between 1 and 65535")

        # Ensure models directory exists
        Path(self.models_dir).mkdir(parents=True, exist_ok=True)

        # Set HuggingFace cache to our models directory
        os.environ["HF_HOME"] = self.models_dir
        os.environ["HUGGINGFACE_HUB_CACHE"] = str(Path(self.models_dir) / "hub")

# Load configuration
config = ServerConfig()

class ModelStatus(str, Enum):
    """Model status enumeration"""
    LOADING = "loading"
    READY = "ready"
    ERROR = "error"
    UNLOADED = "unloaded"

class ModelManager:
    """
    Manages MLX model loading, caching, and inference.

    This class handles the lifecycle of multiple embedding models,
    including loading, warming up, and generating embeddings.

    FEAT-MEM1: Lazy loading with TTL auto-unload
    - Models are loaded on first request, not at startup (if lazy_load=True)
    - Idle models are automatically unloaded after idle_ttl_seconds
    - This reduces idle memory from 15-35GB to <100MB
    """

    def __init__(self, config: ServerConfig):
        self.config = config
        self.models: Dict[str, Tuple[Any, Any]] = {}
        self.model_status: Dict[str, ModelStatus] = {}
        self.model_load_times: Dict[str, float] = {}
        self._locks: Dict[str, asyncio.Lock] = {}
        self._embedding_cache: Dict[str, np.ndarray] = {}
        self._global_lock = asyncio.Lock()
        self.max_loaded_models = 2

        # TTL tracking (FEAT-MEM1)
        self._last_used: Dict[str, float] = {}  # model_name -> timestamp
        self._ttl_task: Optional[asyncio.Task] = None
        self._shutdown = False

    async def start_ttl_checker(self) -> None:
        """Start the background TTL checker task."""
        if self._ttl_task is None and self.config.idle_ttl_seconds > 0:
            self._ttl_task = asyncio.create_task(self._ttl_checker_loop())
            logger.info(f"TTL checker started (idle_ttl={self.config.idle_ttl_seconds}s)")

    async def stop_ttl_checker(self) -> None:
        """Stop the background TTL checker task."""
        self._shutdown = True
        if self._ttl_task is not None:
            self._ttl_task.cancel()
            try:
                await self._ttl_task
            except asyncio.CancelledError:
                pass
            self._ttl_task = None
            logger.info("TTL checker stopped")

    async def _ttl_checker_loop(self) -> None:
        """Background task that unloads idle models after TTL expires."""
        check_interval = min(60, self.config.idle_ttl_seconds // 2)  # Check every minute or half TTL
        logger.info(f"TTL checker loop started (check_interval={check_interval}s)")

        while not self._shutdown:
            try:
                await asyncio.sleep(check_interval)

                if self._shutdown:
                    break

                now = time.time()
                models_to_unload = []

                for model_name, last_used in list(self._last_used.items()):
                    if model_name in self.models:
                        idle_time = now - last_used
                        if idle_time >= self.config.idle_ttl_seconds:
                            models_to_unload.append((model_name, idle_time))

                for model_name, idle_time in models_to_unload:
                    logger.info(f"Unloading idle model {model_name} (idle for {idle_time:.0f}s)")
                    self._unload_model(model_name)
                    del self._last_used[model_name]

            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Error in TTL checker loop: {e}")

    def _update_last_used(self, model_name: str) -> None:
        """Update the last-used timestamp for a model."""
        self._last_used[model_name] = time.time()

    def _resolve_model_name(self, model_identifier: Optional[str] = None) -> str:
        """Resolve model identifier to actual model name"""
        if not model_identifier:
            return self.config.model_name

        model_lower = model_identifier.lower()
        if model_lower in MODEL_ALIASES:
            return MODEL_ALIASES[model_lower]

        if model_identifier in AVAILABLE_MODELS:
            return model_identifier

        raise ValueError(f"Unknown model: {model_identifier}. Available: {list(AVAILABLE_MODELS.keys())}")

    async def load_model(self, model_name: Optional[str] = None) -> str:
        """Load and initialize the specified embedding model"""
        model_name = self._resolve_model_name(model_name)

        if model_name in self.models and self.model_status.get(model_name) == ModelStatus.READY:
            return model_name

        async with self._global_lock:
            if model_name not in self._locks:
                self._locks[model_name] = asyncio.Lock()

        async with self._locks[model_name]:
            if model_name in self.models and self.model_status.get(model_name) == ModelStatus.READY:
                return model_name

            self.model_status[model_name] = ModelStatus.LOADING
            logger.info(f"Loading model: {model_name}")
            logger.info(f"Models directory: {self.config.models_dir}")
            start_time = time.time()

            try:
                await self._manage_memory(model_name)

                model, tokenizer = load(model_name)

                if not hasattr(model, 'model'):
                    raise ValueError("Invalid model architecture: missing 'model' attribute")

                self.models[model_name] = (model, tokenizer)

                logger.info(f"Warming up model {model_name}...")
                await self._warmup(model_name)

                self.model_load_times[model_name] = time.time() - start_time
                self.model_status[model_name] = ModelStatus.READY
                self._update_last_used(model_name)  # Track for TTL
                logger.info(f"Model {model_name} loaded successfully in {self.model_load_times[model_name]:.2f}s")

                return model_name

            except Exception as e:
                self.model_status[model_name] = ModelStatus.ERROR
                logger.error(f"Failed to load model {model_name}: {e}", exc_info=True)
                raise RuntimeError(f"Model loading failed: {e}") from e

    def _unload_model(self, model_name: str) -> None:
        """
        Unload a model and free MLX memory.

        This method properly releases GPU memory by:
        1. Removing model references
        2. Running Python garbage collection
        3. Clearing MLX Metal cache
        4. Synchronizing Metal operations

        See: .aman-pm/backlog/tasks/TASK-MEM2.md for rationale.
        Without explicit cleanup, MLX memory accumulates until system freeze.
        """
        if model_name not in self.models:
            return

        logger.info(f"Unloading model {model_name} and clearing MLX memory")

        # Remove model references
        del self.models[model_name]
        self.model_status[model_name] = ModelStatus.UNLOADED

        # Clear embedding cache for this model
        cache_keys_to_remove = [k for k in self._embedding_cache.keys()
                                if k.startswith(f"{model_name}:")]
        for key in cache_keys_to_remove:
            del self._embedding_cache[key]

        # Force Python garbage collection first
        gc.collect()

        # Clear MLX Metal cache to free GPU memory
        try:
            mx.metal.clear_cache()
            mx.synchronize()
            logger.info(f"MLX memory cleared after unloading {model_name}")
        except Exception as e:
            logger.warning(f"Failed to clear MLX cache (non-critical): {e}")

    async def _manage_memory(self, new_model: str) -> None:
        """Manage memory by evicting models if necessary"""
        if len(self.models) >= self.max_loaded_models:
            models_to_evict = [m for m in self.models.keys() if m != new_model]
            if models_to_evict:
                evict_model = models_to_evict[0]
                logger.info(f"Evicting model {evict_model} to make room for {new_model}")
                self._unload_model(evict_model)

    async def _warmup(self, model_name: str) -> None:
        """Warm up model to compile Metal kernels"""
        try:
            test_texts = ["warmup", "test"]
            model, tokenizer = self.models[model_name]

            for text in test_texts:
                tokens = tokenizer.encode(text)
                if len(tokens) > self.config.max_text_length:
                    tokens = tokens[:self.config.max_text_length]

                input_ids = mx.array([tokens])
                hidden_states = self._get_hidden_states(input_ids, model)
                pooled = mx.mean(hidden_states, axis=1)
                mx.eval(pooled)

        except Exception as e:
            logger.warning(f"Warmup failed for {model_name} (non-critical): {e}")

    def _get_hidden_states(self, input_ids: mx.array, model: Any) -> mx.array:
        """Extract hidden states from the model before output projection."""
        h = model.model.embed_tokens(input_ids)

        for layer in model.model.layers:
            h = layer(h, mask=None, cache=None)

        h = model.model.norm(h)

        return h

    async def generate_embeddings(
        self,
        texts: List[str],
        model_name: Optional[str] = None,
        normalize: bool = True
    ) -> Tuple[np.ndarray, str, int]:
        """Generate embeddings for a list of texts."""
        model_name = await self.load_model(model_name)
        self._update_last_used(model_name)  # Refresh TTL on every request

        if self.model_status.get(model_name) != ModelStatus.READY:
            raise RuntimeError(f"Model {model_name} not ready (status: {self.model_status.get(model_name)})")

        if not texts:
            embedding_dim = AVAILABLE_MODELS[model_name]["embedding_dim"]
            return np.array([]), model_name, embedding_dim

        model, tokenizer = self.models[model_name]
        embedding_dim = AVAILABLE_MODELS[model_name]["embedding_dim"]

        embeddings = []

        for text in texts:
            cache_key = f"{model_name}:{text}:{normalize}"
            if cache_key in self._embedding_cache:
                embeddings.append(self._embedding_cache[cache_key])
                continue

            tokens = tokenizer.encode(text)

            if len(tokens) > self.config.max_text_length:
                logger.warning(f"Truncating text from {len(tokens)} to {self.config.max_text_length} tokens")
                tokens = tokens[:self.config.max_text_length]

            input_ids = mx.array([tokens])
            hidden_states = self._get_hidden_states(input_ids, model)
            pooled = mx.mean(hidden_states, axis=1)

            if normalize:
                norm = mx.linalg.norm(pooled, axis=1, keepdims=True)
                pooled = pooled / mx.maximum(norm, 1e-9)

            mx.eval(pooled)
            embedding = np.array(pooled.tolist()[0], dtype=np.float32)

            if len(self._embedding_cache) < 1000:
                self._embedding_cache[cache_key] = embedding

            embeddings.append(embedding)

        return np.array(embeddings, dtype=np.float32), model_name, embedding_dim

    def get_status(self, model_name: Optional[str] = None) -> Dict[str, Any]:
        """Get current model status and information"""
        if model_name:
            model_name = self._resolve_model_name(model_name)
            return {
                "status": self.model_status.get(model_name, ModelStatus.UNLOADED).value,
                "model_name": model_name,
                "embedding_dim": AVAILABLE_MODELS[model_name]["embedding_dim"],
                "load_time": self.model_load_times.get(model_name),
                "description": AVAILABLE_MODELS[model_name]["description"]
            }

        models_status = {}
        now = time.time()
        for name in AVAILABLE_MODELS:
            status_entry = {
                "status": self.model_status.get(name, ModelStatus.UNLOADED).value,
                "embedding_dim": AVAILABLE_MODELS[name]["embedding_dim"],
                "load_time": self.model_load_times.get(name),
                "aliases": AVAILABLE_MODELS[name]["alias"],
                "description": AVAILABLE_MODELS[name]["description"]
            }
            # Add TTL info for loaded models
            if name in self._last_used and name in self.models:
                idle_time = now - self._last_used[name]
                remaining_ttl = max(0, self.config.idle_ttl_seconds - idle_time)
                status_entry["idle_seconds"] = int(idle_time)
                status_entry["ttl_remaining_seconds"] = int(remaining_ttl)
            models_status[name] = status_entry

        return {
            "loaded_models": list(self.models.keys()),
            "default_model": self.config.model_name,
            "max_batch_size": self.config.max_batch_size,
            "max_text_length": self.config.max_text_length,
            "cache_size": len(self._embedding_cache),
            "models_dir": self.config.models_dir,
            "lazy_load": self.config.lazy_load,
            "idle_ttl_seconds": self.config.idle_ttl_seconds,
            "models": models_status
        }

# Initialize model manager
model_manager = ModelManager(config)


class RerankerManager:
    """
    Manages Qwen3-Reranker model for cross-encoder scoring (FEAT-RR1).

    Cross-encoders score query-document pairs jointly, providing more accurate
    relevance scores than bi-encoders but at higher computational cost.

    Qwen3-Reranker uses a yes/no judgment approach:
    - Input: Formatted prompt with query + document
    - Output: Logits for "yes" and "no" tokens
    - Score: softmax probability of "yes" (0.0 to 1.0)
    """

    # Qwen3 chat template tokens
    SYSTEM_PROMPT = (
        "Judge whether the Document meets the requirements based on the Query "
        "and the Instruct provided. Note that the answer can only be \"yes\" or \"no\"."
    )

    DEFAULT_INSTRUCTION = (
        "Given a search query, retrieve relevant code or documentation that answers the query"
    )

    def __init__(self, config: ServerConfig):
        self.config = config
        self.models: Dict[str, Tuple[Any, Any]] = {}  # model_name -> (model, tokenizer)
        self.model_status: Dict[str, ModelStatus] = {}
        self.model_load_times: Dict[str, float] = {}
        self._locks: Dict[str, asyncio.Lock] = {}
        self._global_lock = asyncio.Lock()
        self._last_used: Dict[str, float] = {}
        self._token_ids: Dict[str, Tuple[int, int]] = {}  # model -> (yes_id, no_id)
        self.max_loaded_models = 1  # Reranker is memory-intensive

    def _resolve_model_name(self, model_identifier: Optional[str] = None) -> str:
        """Resolve model identifier to actual reranker model name."""
        if not model_identifier:
            return DEFAULT_RERANKER_MODEL

        model_lower = model_identifier.lower()
        if model_lower in RERANKER_ALIASES:
            return RERANKER_ALIASES[model_lower]

        if model_identifier in AVAILABLE_RERANKER_MODELS:
            return model_identifier

        raise ValueError(
            f"Unknown reranker model: {model_identifier}. "
            f"Available: {list(AVAILABLE_RERANKER_MODELS.keys())}"
        )

    async def load_model(self, model_name: Optional[str] = None) -> str:
        """Load and initialize the specified reranker model."""
        model_name = self._resolve_model_name(model_name)

        if model_name in self.models and self.model_status.get(model_name) == ModelStatus.READY:
            return model_name

        async with self._global_lock:
            if model_name not in self._locks:
                self._locks[model_name] = asyncio.Lock()

        async with self._locks[model_name]:
            if model_name in self.models and self.model_status.get(model_name) == ModelStatus.READY:
                return model_name

            self.model_status[model_name] = ModelStatus.LOADING
            logger.info(f"Loading reranker model: {model_name}")
            start_time = time.time()

            try:
                # Manage memory - unload other reranker models
                await self._manage_memory(model_name)

                # Load model using mlx_lm
                model, tokenizer = load(model_name)

                # Get yes/no token IDs for scoring
                yes_id = tokenizer.convert_tokens_to_ids("yes")
                no_id = tokenizer.convert_tokens_to_ids("no")

                if yes_id is None or no_id is None:
                    # Fallback: try lowercase
                    yes_id = tokenizer.encode("yes", add_special_tokens=False)[-1]
                    no_id = tokenizer.encode("no", add_special_tokens=False)[-1]

                self._token_ids[model_name] = (yes_id, no_id)
                self.models[model_name] = (model, tokenizer)

                # Warmup
                logger.info(f"Warming up reranker {model_name}...")
                await self._warmup(model_name)

                self.model_load_times[model_name] = time.time() - start_time
                self.model_status[model_name] = ModelStatus.READY
                self._last_used[model_name] = time.time()
                logger.info(
                    f"Reranker {model_name} loaded in {self.model_load_times[model_name]:.2f}s "
                    f"(yes_id={yes_id}, no_id={no_id})"
                )

                return model_name

            except Exception as e:
                self.model_status[model_name] = ModelStatus.ERROR
                logger.error(f"Failed to load reranker {model_name}: {e}", exc_info=True)
                raise RuntimeError(f"Reranker loading failed: {e}") from e

    def _unload_model(self, model_name: str) -> None:
        """Unload a reranker model and free memory."""
        if model_name not in self.models:
            return

        logger.info(f"Unloading reranker {model_name}")
        del self.models[model_name]
        if model_name in self._token_ids:
            del self._token_ids[model_name]
        self.model_status[model_name] = ModelStatus.UNLOADED

        gc.collect()
        try:
            mx.metal.clear_cache()
            mx.synchronize()
        except Exception as e:
            logger.warning(f"Failed to clear MLX cache: {e}")

    async def _manage_memory(self, new_model: str) -> None:
        """Evict models if needed to make room for new one."""
        if len(self.models) >= self.max_loaded_models:
            models_to_evict = [m for m in self.models.keys() if m != new_model]
            if models_to_evict:
                evict_model = models_to_evict[0]
                logger.info(f"Evicting reranker {evict_model} for {new_model}")
                self._unload_model(evict_model)

    async def _warmup(self, model_name: str) -> None:
        """Warm up reranker with test query-document pair.

        Note: This method is called while holding the model lock during load_model(),
        so we cannot call rerank() directly (would cause deadlock). Instead, we
        directly run the model forward pass.
        """
        try:
            model, tokenizer = self.models[model_name]
            yes_id, no_id = self._token_ids[model_name]

            # Simple warmup: format and run a test pair
            formatted = self._format_pair("warmup query", "warmup document")
            tokens = tokenizer.encode(formatted, add_special_tokens=False)
            input_ids = mx.array([tokens[:512]])  # Limit length for warmup

            # Run forward pass to compile Metal kernels
            logits = model(input_ids)
            mx.eval(logits)  # Force evaluation

            logger.info(f"Reranker {model_name} warmup complete")
        except Exception as e:
            logger.warning(f"Reranker warmup failed (non-critical): {e}")

    def _format_pair(self, query: str, document: str, instruction: Optional[str] = None) -> str:
        """
        Format a query-document pair using Qwen3 chat template.

        Template:
        <|im_start|>system
        {SYSTEM_PROMPT}
        <|im_end|>
        <|im_start|>user
        <Instruct>: {instruction}
        <Query>: {query}
        <Document>: {document}
        <|im_end|>
        <|im_start|>assistant
        <think>

        </think>

        """
        if instruction is None:
            instruction = self.DEFAULT_INSTRUCTION

        return (
            f"<|im_start|>system\n{self.SYSTEM_PROMPT}<|im_end|>\n"
            f"<|im_start|>user\n"
            f"<Instruct>: {instruction}\n"
            f"<Query>: {query}\n"
            f"<Document>: {document}\n"
            f"<|im_end|>\n"
            f"<|im_start|>assistant\n"
            f"<think>\n\n</think>\n\n"
        )

    async def rerank(
        self,
        query: str,
        documents: List[str],
        model_name: Optional[str] = None,
        instruction: Optional[str] = None,
        top_k: Optional[int] = None
    ) -> List[Tuple[int, float, str]]:
        """
        Rerank documents by relevance to query.

        Args:
            query: Search query
            documents: List of documents to rerank
            model_name: Reranker model to use
            instruction: Custom instruction (default: code/doc retrieval)
            top_k: Return only top K results (default: all)

        Returns:
            List of (original_index, score, document) tuples, sorted by score descending
        """
        model_name = await self.load_model(model_name)
        self._last_used[model_name] = time.time()

        if not documents:
            return []

        model, tokenizer = self.models[model_name]
        yes_id, no_id = self._token_ids[model_name]
        max_length = AVAILABLE_RERANKER_MODELS[model_name]["max_length"]

        results = []

        for idx, doc in enumerate(documents):
            try:
                # Format the pair
                formatted = self._format_pair(query, doc, instruction)

                # Tokenize
                tokens = tokenizer.encode(formatted, add_special_tokens=False)
                if len(tokens) > max_length:
                    logger.warning(f"Truncating rerank input from {len(tokens)} to {max_length}")
                    tokens = tokens[:max_length]

                # Run model
                input_ids = mx.array([tokens])
                logits = model(input_ids)

                # Get last token logits (the prediction)
                last_logits = logits[:, -1, :]

                # Extract yes/no logits and compute score
                yes_logit = float(last_logits[0, yes_id])
                no_logit = float(last_logits[0, no_id])

                # Softmax to get probability
                max_logit = max(yes_logit, no_logit)
                yes_exp = np.exp(yes_logit - max_logit)
                no_exp = np.exp(no_logit - max_logit)
                score = yes_exp / (yes_exp + no_exp)

                results.append((idx, score, doc))

            except Exception as e:
                logger.warning(f"Failed to score document {idx}: {e}")
                results.append((idx, 0.0, doc))

        # Sort by score descending
        results.sort(key=lambda x: x[1], reverse=True)

        # Apply top_k if specified
        if top_k is not None and top_k > 0:
            results = results[:top_k]

        return results

    def get_status(self) -> Dict[str, Any]:
        """Get reranker status information."""
        models_status = {}
        now = time.time()

        for name in AVAILABLE_RERANKER_MODELS:
            status_entry = {
                "status": self.model_status.get(name, ModelStatus.UNLOADED).value,
                "max_length": AVAILABLE_RERANKER_MODELS[name]["max_length"],
                "load_time": self.model_load_times.get(name),
                "aliases": AVAILABLE_RERANKER_MODELS[name]["alias"],
                "description": AVAILABLE_RERANKER_MODELS[name]["description"]
            }
            if name in self._last_used and name in self.models:
                idle_time = now - self._last_used[name]
                status_entry["idle_seconds"] = int(idle_time)
            models_status[name] = status_entry

        return {
            "loaded_models": list(self.models.keys()),
            "default_model": DEFAULT_RERANKER_MODEL,
            "models": models_status
        }


# Initialize reranker manager (FEAT-RR1)
reranker_manager = RerankerManager(config)


# Pydantic models with validation
class EmbedRequest(BaseModel):
    """Single text embedding request"""
    model_config = ConfigDict(str_strip_whitespace=True)

    text: str = Field(
        ...,
        description="Text to embed",
        min_length=1,
        max_length=config.max_text_length * 10
    )
    model: Optional[str] = Field(
        default=None,
        description="Model to use (name or alias like 'small', 'large'). Defaults to configured model."
    )
    normalize: bool = Field(
        default=True,
        description="Apply L2 normalization to embeddings"
    )

    @field_validator('text')
    def validate_text(cls, v):
        if not v or v.isspace():
            raise ValueError("Text cannot be empty or whitespace only")
        return v

class EmbedResponse(BaseModel):
    """Single embedding response"""
    embedding: List[float] = Field(..., description="Embedding vector")
    model: str = Field(..., description="Model name used")
    dim: int = Field(..., description="Embedding dimension")
    normalized: bool = Field(..., description="Whether embedding is normalized")
    processing_time_ms: float = Field(..., description="Processing time in milliseconds")

class BatchEmbedRequest(BaseModel):
    """Batch embedding request"""
    model_config = ConfigDict(str_strip_whitespace=True)

    texts: List[str] = Field(
        ...,
        description="List of texts to embed",
        min_length=1,
        max_length=1024
    )
    model: Optional[str] = Field(
        default=None,
        description="Model to use (name or alias like 'small', 'large'). Defaults to configured model."
    )
    normalize: bool = Field(
        default=True,
        description="Apply L2 normalization to embeddings"
    )

    @field_validator('texts')
    def validate_texts(cls, v):
        if not v:
            raise ValueError("Text list cannot be empty")
        for i, text in enumerate(v):
            if not text or text.isspace():
                raise ValueError(f"Text at index {i} cannot be empty or whitespace only")
        return v

class BatchEmbedResponse(BaseModel):
    """Batch embedding response"""
    embeddings: List[List[float]] = Field(..., description="List of embedding vectors")
    model: str = Field(..., description="Model name used")
    dim: int = Field(..., description="Embedding dimension")
    count: int = Field(..., description="Number of embeddings")
    normalized: bool = Field(..., description="Whether embeddings are normalized")
    processing_time_ms: float = Field(..., description="Processing time in milliseconds")

class HealthResponse(BaseModel):
    """Health check response"""
    status: str = Field(..., description="Service health status")
    model_status: str = Field(..., description="Model status")
    loaded_model: str = Field(..., description="Model name")
    embedding_dim: int = Field(..., description="Embedding dimension")
    models_dir: str = Field(..., description="Models storage directory")
    memory_usage_mb: Optional[float] = Field(None, description="Memory usage in MB")
    uptime_seconds: float = Field(..., description="Service uptime in seconds")


# Reranker Pydantic models (FEAT-RR1)
class RerankRequest(BaseModel):
    """Cross-encoder reranking request"""
    model_config = ConfigDict(str_strip_whitespace=True)

    query: str = Field(
        ...,
        description="Search query to rank documents against",
        min_length=1,
        max_length=8192
    )
    documents: List[str] = Field(
        ...,
        description="List of documents to rerank",
        min_length=1,
        max_length=100  # Limit to 100 for latency
    )
    model: Optional[str] = Field(
        default=None,
        description="Reranker model to use (default: reranker-0.6b)"
    )
    instruction: Optional[str] = Field(
        default=None,
        description="Custom instruction for reranking (default: code/doc retrieval)"
    )
    top_k: Optional[int] = Field(
        default=None,
        description="Return only top K results (default: all)",
        ge=1,
        le=100
    )

    @field_validator('query')
    def validate_query(cls, v):
        if not v or v.isspace():
            raise ValueError("Query cannot be empty or whitespace only")
        return v

    @field_validator('documents')
    def validate_documents(cls, v):
        if not v:
            raise ValueError("Documents list cannot be empty")
        for i, doc in enumerate(v):
            if not doc or doc.isspace():
                raise ValueError(f"Document at index {i} cannot be empty or whitespace only")
        return v


class RerankResult(BaseModel):
    """Single rerank result"""
    index: int = Field(..., description="Original index in input documents list")
    score: float = Field(..., description="Relevance score (0.0 to 1.0)")
    document: str = Field(..., description="Document content")


class RerankResponse(BaseModel):
    """Cross-encoder reranking response"""
    results: List[RerankResult] = Field(..., description="Reranked results sorted by score")
    model: str = Field(..., description="Reranker model used")
    query: str = Field(..., description="Original query")
    count: int = Field(..., description="Number of results returned")
    processing_time_ms: float = Field(..., description="Processing time in milliseconds")


# Application lifespan management
@asynccontextmanager
async def lifespan(app: FastAPI):
    """
    Manage application lifecycle.

    FEAT-MEM1: Lazy loading support
    - If lazy_load=True (default), models are NOT loaded at startup
    - Models load on first request, reducing startup time and idle memory
    - TTL checker runs in background to unload idle models
    """
    logger.info(f"Starting AmanMCP MLX Embedding Server v{app.version}")
    logger.info(f"Port: {config.port}")
    logger.info(f"Models directory: {config.models_dir}")
    logger.info(f"Default model: {config.model_name}")
    logger.info(f"Lazy loading: {config.lazy_load}")
    logger.info(f"Idle TTL: {config.idle_ttl_seconds}s")
    logger.info(f"Available models: {list(AVAILABLE_MODELS.keys())}")

    # Start TTL checker for idle model cleanup
    await model_manager.start_ttl_checker()

    # Only preload model if lazy_load is disabled
    if not config.lazy_load:
        try:
            logger.info(f"Preloading model (lazy_load=False)...")
            await model_manager.load_model(config.model_name)
        except Exception as e:
            logger.error(f"Failed to preload model: {e}")
    else:
        logger.info("Lazy loading enabled - model will load on first request")

    app.state.start_time = time.time()

    yield

    # Cleanup
    logger.info("Shutting down server...")
    await model_manager.stop_ttl_checker()

# Create FastAPI application
app = FastAPI(
    title="AmanMCP MLX Embedding Server",
    description="High-performance text embedding service using MLX on Apple Silicon (bundled with AmanMCP)",
    version="1.3.0-amanmcp",  # FEAT-RR1: Added cross-encoder reranking
    lifespan=lifespan,
    docs_url="/docs",
    redoc_url="/redoc"
)

# Add CORS middleware if enabled
if config.enable_cors:
    app.add_middleware(
        CORSMiddleware,
        allow_origins=config.cors_origins,
        allow_credentials=True,
        allow_methods=["GET", "POST"],
        allow_headers=["*"],
    )

# Request logging middleware
@app.middleware("http")
async def log_requests(request: Request, call_next):
    """Log HTTP requests with timing"""
    start_time = time.time()

    try:
        response = await call_next(request)
        process_time = (time.time() - start_time) * 1000

        logger.info(
            f"{request.method} {request.url.path} "
            f"- Status: {response.status_code} "
            f"- Time: {process_time:.2f}ms"
        )

        response.headers["X-Process-Time"] = str(process_time)
        return response

    except Exception as e:
        process_time = (time.time() - start_time) * 1000
        logger.error(
            f"{request.method} {request.url.path} "
            f"- Error: {e} "
            f"- Time: {process_time:.2f}ms"
        )
        raise

# API Routes
@app.get("/", tags=["General"])
async def root():
    """Get API information"""
    return {
        "service": "AmanMCP MLX Embedding Server",
        "version": app.version,
        "default_model": config.model_name,
        "default_reranker": DEFAULT_RERANKER_MODEL,
        "models_dir": config.models_dir,
        "available_models": list(AVAILABLE_MODELS.keys()),
        "available_rerankers": list(AVAILABLE_RERANKER_MODELS.keys()),
        "endpoints": {
            "embeddings": "/embed",
            "batch_embeddings": "/embed_batch",
            "rerank": "/rerank",
            "health": "/health",
            "metrics": "/metrics",
            "models": "/models",
            "documentation": "/docs"
        }
    }

@app.post(
    "/embed",
    response_model=EmbedResponse,
    tags=["Embeddings"],
    status_code=status.HTTP_200_OK
)
async def embed_single(request: EmbedRequest):
    """Generate embedding for a single text."""
    try:
        start_time = time.time()

        embeddings, model_used, embedding_dim = await model_manager.generate_embeddings(
            [request.text],
            model_name=request.model,
            normalize=request.normalize
        )

        processing_time = (time.time() - start_time) * 1000

        return EmbedResponse(
            embedding=embeddings[0].tolist(),
            model=model_used,
            dim=embedding_dim,
            normalized=request.normalize,
            processing_time_ms=processing_time
        )

    except Exception as e:
        logger.error(f"Embedding generation failed: {e}", exc_info=True)
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Embedding generation failed: {str(e)}"
        )

@app.post(
    "/embed_batch",
    response_model=BatchEmbedResponse,
    tags=["Embeddings"],
    status_code=status.HTTP_200_OK
)
async def embed_batch(request: BatchEmbedRequest):
    """Generate embeddings for multiple texts."""
    try:
        start_time = time.time()

        embeddings, model_used, embedding_dim = await model_manager.generate_embeddings(
            request.texts,
            model_name=request.model,
            normalize=request.normalize
        )

        processing_time = (time.time() - start_time) * 1000

        return BatchEmbedResponse(
            embeddings=embeddings.tolist(),
            model=model_used,
            dim=embedding_dim,
            count=len(embeddings),
            normalized=request.normalize,
            processing_time_ms=processing_time
        )

    except Exception as e:
        logger.error(f"Batch embedding generation failed: {e}", exc_info=True)
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Batch embedding generation failed: {str(e)}"
        )


@app.post(
    "/rerank",
    response_model=RerankResponse,
    tags=["Reranking"],
    status_code=status.HTTP_200_OK
)
async def rerank(request: RerankRequest):
    """
    Rerank documents by relevance to query using cross-encoder.

    Cross-encoders jointly encode query-document pairs, providing more accurate
    relevance scores than bi-encoders. Use this to refine search results.

    **Latency:** ~1-2ms per document on Apple Silicon with MLX.
    **Recommended:** Rerank top 20-50 candidates from initial retrieval.
    """
    try:
        start_time = time.time()

        results = await reranker_manager.rerank(
            query=request.query,
            documents=request.documents,
            model_name=request.model,
            instruction=request.instruction,
            top_k=request.top_k
        )

        processing_time = (time.time() - start_time) * 1000

        return RerankResponse(
            results=[
                RerankResult(index=idx, score=score, document=doc)
                for idx, score, doc in results
            ],
            model=reranker_manager._resolve_model_name(request.model),
            query=request.query,
            count=len(results),
            processing_time_ms=processing_time
        )

    except Exception as e:
        logger.error(f"Reranking failed: {e}", exc_info=True)
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Reranking failed: {str(e)}"
        )


@app.get(
    "/health",
    response_model=HealthResponse,
    tags=["Monitoring"],
    status_code=status.HTTP_200_OK
)
async def health_check():
    """Health check endpoint."""
    try:
        memory_mb = None
        try:
            import psutil
            process = psutil.Process()
            memory_mb = process.memory_info().rss / 1024 / 1024
        except ImportError:
            pass

        uptime = time.time() - app.state.start_time if hasattr(app.state, 'start_time') else 0

        default_model_status = model_manager.model_status.get(
            config.model_name, ModelStatus.UNLOADED
        )

        # Determine health status
        # With lazy loading, UNLOADED is healthy (model loads on first request)
        # Only ERROR or LOADING are degraded
        if default_model_status == ModelStatus.READY:
            health_status = "healthy"
        elif default_model_status == ModelStatus.UNLOADED and config.lazy_load:
            health_status = "healthy"  # Model will load on first request
        else:
            health_status = "degraded"

        return HealthResponse(
            status=health_status,
            model_status=default_model_status.value,
            loaded_model=config.model_name,
            embedding_dim=AVAILABLE_MODELS[config.model_name]["embedding_dim"],
            models_dir=config.models_dir,
            memory_usage_mb=memory_mb,
            uptime_seconds=uptime
        )

    except Exception as e:
        logger.error(f"Health check failed: {e}", exc_info=True)
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail=f"Health check failed: {str(e)}"
        )

@app.get("/metrics", tags=["Monitoring"])
async def get_metrics():
    """Get detailed metrics and configuration."""
    return {
        "models": model_manager.get_status(),
        "config": {
            "host": config.host,
            "port": config.port,
            "max_batch_size": config.max_batch_size,
            "max_text_length": config.max_text_length,
            "cors_enabled": config.enable_cors,
            "models_dir": config.models_dir
        },
        "version": app.version
    }

@app.get("/models", tags=["Models"])
async def list_models():
    """List available embedding and reranker models and their status."""
    return {
        "embeddings": model_manager.get_status(),
        "rerankers": reranker_manager.get_status()
    }

# Error handlers
@app.exception_handler(ValueError)
async def value_error_handler(request: Request, exc: ValueError):
    """Handle validation errors"""
    return JSONResponse(
        status_code=status.HTTP_400_BAD_REQUEST,
        content={"detail": str(exc)}
    )

@app.exception_handler(Exception)
async def general_exception_handler(request: Request, exc: Exception):
    """Handle unexpected errors"""
    logger.error(f"Unexpected error: {exc}", exc_info=True)
    return JSONResponse(
        status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
        content={"detail": "An unexpected error occurred"}
    )

# Filter for uvicorn access logs to exclude health checks
class UvicornHealthFilter(logging.Filter):
    """Filter to exclude /health endpoint from uvicorn access logs."""

    def filter(self, record: logging.LogRecord) -> bool:
        # Uvicorn access log format includes the path
        msg = record.getMessage()
        if '/health' in msg:
            return False
        return True


# Main entry point
def main():
    """Run the server"""
    # Add health check filter to uvicorn's access logger
    uvicorn_access = logging.getLogger("uvicorn.access")
    uvicorn_access.addFilter(UvicornHealthFilter())

    uvicorn.run(
        "server:app",
        host=config.host,
        port=config.port,
        log_level=os.getenv("LOG_LEVEL", "info").lower(),
        reload=os.getenv("DEV_MODE", "false").lower() == "true",
        access_log=True
    )

if __name__ == "__main__":
    main()
