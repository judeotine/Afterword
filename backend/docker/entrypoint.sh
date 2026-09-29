#!/bin/bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_debug() {
    if [ "${WHISPER_DEBUG:-false}" = "true" ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

WHISPER_MODEL=${WHISPER_MODEL:-models/ggml-base.en.bin}
WHISPER_HOST=${WHISPER_HOST:-0.0.0.0}
WHISPER_PORT=${WHISPER_PORT:-8178}
WHISPER_THREADS=${WHISPER_THREADS:-0}
WHISPER_USE_GPU=${WHISPER_USE_GPU:-true}
WHISPER_LANGUAGE=${WHISPER_LANGUAGE:-en}
WHISPER_TRANSLATE=${WHISPER_TRANSLATE:-false}
WHISPER_DIARIZE=${WHISPER_DIARIZE:-false}
WHISPER_PRINT_PROGRESS=${WHISPER_PRINT_PROGRESS:-true}

detect_gpu_silent() {

    if command -v nvidia-smi >/dev/null 2>&1; then
        if nvidia-smi >/dev/null 2>&1; then
            echo "nvidia"
            return 0
        fi
    fi

    if command -v rocm-smi >/dev/null 2>&1; then
        if rocm-smi >/dev/null 2>&1; then
            echo "amd"
            return 0
        fi
    fi

    if [ -d /dev/dri ]; then
        if ls /dev/dri/render* >/dev/null 2>&1; then
            echo "intel"
            return 0
        fi
    fi

    echo "cpu"
    return 0
}

detect_gpu() {
    log_info "Detecting available GPU hardware..."

    if [ "${WHISPER_PLATFORM:-}" = "macos" ]; then
        log_info "🍎 macOS container - GPU acceleration disabled (Docker limitation)"
        log_info "💡 For GPU acceleration on macOS, use the native approach:"
        log_info "   ./clean_start_backend.sh"
        echo "cpu"
        return 0
    fi

    local gpu_type
    gpu_type=$(detect_gpu_silent)

    case "$gpu_type" in
        "nvidia")
            local gpu_count=$(nvidia-smi --query-gpu=name --format=csv,noheader,nounits | wc -l)
            log_info "Found $gpu_count NVIDIA GPU(s):"
            nvidia-smi --query-gpu=name,memory.total --format=csv,noheader,nounits | while read -r line; do
                log_info "  - $line"
            done
            ;;
        "amd")
            log_info "AMD GPU detected (ROCm)"
            ;;
        "intel")
            log_info "Intel GPU detected"
            ;;
        "cpu")
            log_info "No GPU detected, will use CPU"
            ;;
    esac

    echo "$gpu_type"
    return 0
}

set_optimal_threads() {
    if [ "$WHISPER_THREADS" = "0" ] || [ -z "$WHISPER_THREADS" ]; then

        local cpu_cores=$(nproc)
        local optimal_threads=$((cpu_cores > 8 ? 8 : cpu_cores))
        log_info "Auto-setting threads to $optimal_threads (detected $cpu_cores CPU cores)"
        WHISPER_THREADS=$optimal_threads
    else
        log_info "Using configured thread count: $WHISPER_THREADS"
    fi
}

show_download_info() {
    local model_size="$1"

    case "$model_size" in
        tiny*)
            log_info "📦 Model size: ~39 MB (fastest, least accurate)"
            log_info "⏱️  Estimated download time: ~10 seconds on fast connection"
            ;;
        base*)
            log_info "📦 Model size: ~142 MB (good balance of speed/accuracy)"
            log_info "⏱️  Estimated download time: ~30 seconds on fast connection"
            ;;
        small*)
            log_info "📦 Model size: ~244 MB (better accuracy)"
            log_info "⏱️  Estimated download time: ~1 minute on fast connection"
            ;;
        medium*)
            log_info "📦 Model size: ~769 MB (high accuracy)"
            log_info "⏱️  Estimated download time: ~3 minutes on fast connection"
            ;;
        large*)
            log_info "📦 Model size: ~1550 MB (best accuracy, slowest)"
            log_info "⏱️  Estimated download time: ~5-8 minutes on fast connection"
            ;;
        *)
            log_info "📦 Model size: Unknown"
            ;;
    esac
}

download_model_with_progress() {
    local model_path="$1"
    local download_url="$2"
    local model_size="$3"

    log_info "🌐 Starting download from HuggingFace..."
    log_info "📋 URL: $download_url"

    show_download_info "$model_size"

    echo -e "${BLUE}Download Progress:${NC}"

    if curl -L -f \
        --progress-bar \
        --connect-timeout 30 \
        --max-time 3600 \
        --retry 3 \
        --retry-delay 5 \
        --retry-connrefused \
        -o "$model_path" \
        "$download_url" 2>&1 | while IFS= read -r line; do

            if [[ "$line" =~ \#+ ]]; then
                echo -ne "\r${GREEN}Progress: $line${NC}"
            fi
        done; then
        echo -e "\n${GREEN}✅ Download completed successfully!${NC}"

        local file_size=$(du -h "$model_path" | cut -f1)
        log_info "📁 Downloaded file size: $file_size"

        if [ -s "$model_path" ]; then
            log_info "✅ Model file validation passed"
            return 0
        else
            log_error "❌ Downloaded file appears to be empty or corrupted"
            rm -f "$model_path"
            return 1
        fi
    else
        echo -e "\n${RED}❌ Download failed${NC}"
        return 1
    fi
}

ensure_model() {
    local model_path="$1"

    log_info "🔍 Checking model availability: $model_path"

    if [ -f "$model_path" ]; then
        local file_size=$(du -h "$model_path" | cut -f1)
        log_info "✅ Model found: $model_path ($file_size)"
        return 0
    fi

    if [ "${WHISPER_PLATFORM:-}" = "macos" ]; then
        log_info "🍎 macOS container detected - checking volume mounts..."

        if [ -d "/app/models" ]; then
            log_info "📁 Contents of /app/models:"
            ls -la /app/models/ || log_warn "Cannot list models directory"

            local available_models=$(find /app/models -name "*.bin" -type f 2>/dev/null | head -5)
            if [ -n "$available_models" ]; then
                log_info "🔍 Available models found:"
                echo "$available_models" | while read -r model; do
                    local size=$(du -h "$model" | cut -f1)
                    log_info "  $model ($size)"
                done

                local first_available=$(echo "$available_models" | head -1)
                if [ -n "$first_available" ]; then
                    log_warn "⚠️  Requested model not found, but other models are available"
                    log_info "💡 Consider updating WHISPER_MODEL environment variable to:"
                    log_info "   WHISPER_MODEL=$(basename "$first_available")"
                fi
            fi
        else
            log_error "❌ Models directory not found at /app/models"
            log_error "💡 For macOS, ensure you have:"
            log_error "   - Built the image with: ./build-docker.sh macos"
            log_error "   - Started with: docker-compose --profile macos up"
        fi
    fi

    local model_name=$(basename "$model_path")
    if [ -f "/app/local_models/$model_name" ]; then
        log_info "📂 Model found in local_models, copying to models directory..."
        mkdir -p "$(dirname "$model_path")"
        cp "/app/local_models/$model_name" "$model_path"
        local file_size=$(du -h "$model_path" | cut -f1)
        log_info "✅ Model copied successfully ($file_size)"
        return 0
    fi

    log_warn "❌ Model not found locally: $model_path"
    local model_basename=$(basename "$model_path" .bin)

    local model_size=""
    if [[ "$model_basename" =~ ggml-(.+) ]]; then
        model_size="${BASH_REMATCH[1]}"
        log_info "🔄 Attempting to download model: $model_size"

        mkdir -p "$(dirname "$model_path")"

        local download_url="https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-${model_size}.bin"

        if download_model_with_progress "$model_path" "$download_url" "$model_size"; then
            log_info "🎉 Model is ready for use!"
            return 0
        else
            log_error "💥 Failed to download model from $download_url"
            rm -f "$model_path"
        fi
    fi

    log_error "❌ Model not available and could not be downloaded: $model_path"
    echo
    log_error "💡 Available options:"
    log_error "   1. Mount model directory: -v /path/to/models:/app/models"
    log_error "   2. Place model in local_models directory"
    log_error "   3. Use model-downloader service in docker-compose.yml"
    log_error "   4. Pre-download models using: ./run-docker.sh models download $model_size"
    echo

    if [[ "$model_size" != "tiny.en" && "$model_size" != "base.en" ]]; then
        log_warn "🔄 Attempting fallback to base.en model..."
        local fallback_path="models/ggml-base.en.bin"
        local fallback_url="https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin"

        if download_model_with_progress "$fallback_path" "$fallback_url" "base.en"; then
            log_info "✅ Fallback model downloaded successfully!"
            log_warn "⚠️  Using base.en instead of requested $model_size"

            WHISPER_MODEL="$fallback_path"
            return 0
        else
            log_error "❌ Fallback model download also failed"
        fi
    fi

    return 1
}

build_server_args() {
    local args=()

    local gpu_type
    gpu_type=$(detect_gpu_silent)

    args+=("--model" "$WHISPER_MODEL")
    args+=("--host" "$WHISPER_HOST")
    args+=("--port" "$WHISPER_PORT")
    args+=("--threads" "$WHISPER_THREADS")

    if [ "$WHISPER_USE_GPU" = "true" ] && [ "$gpu_type" != "cpu" ]; then
        args+=("--use-gpu")
    fi

    if [ "$WHISPER_LANGUAGE" != "auto" ] && [ -n "$WHISPER_LANGUAGE" ]; then
        args+=("--language" "$WHISPER_LANGUAGE")
    fi

    [ "$WHISPER_TRANSLATE" = "true" ] && args+=("--translate")
    [ "$WHISPER_DIARIZE" = "true" ] && args+=("--diarize")
    [ "$WHISPER_PRINT_PROGRESS" = "true" ] && args+=("--print-progress")

    echo "${args[@]}"
}

start_server() {
    echo
    log_info "🚀 Starting Whisper Server..."
    echo

    local gpu_type
    gpu_type=$(detect_gpu)

    set_optimal_threads

    echo
    if ! ensure_model "$WHISPER_MODEL"; then
        log_error "❌ Cannot start server without a valid model"
        exit 1
    fi

    local server_args
    server_args=$(build_server_args "$gpu_type")

    echo
    log_info "📋 Server configuration:"
    log_info "   Model: $WHISPER_MODEL"
    log_info "   Host: $WHISPER_HOST"
    log_info "   Port: $WHISPER_PORT"
    log_info "   Threads: $WHISPER_THREADS"
    if [ "$WHISPER_USE_GPU" = "true" ] && [ "$gpu_type" != "cpu" ]; then
        log_info "   GPU: $gpu_type (enabled)"
    else
        log_info "   GPU: cpu (enabled)"
    fi
    log_info "   Language: $WHISPER_LANGUAGE"

    local features=()
    [ "$WHISPER_TRANSLATE" = "true" ] && features+=("Translation")
    [ "$WHISPER_DIARIZE" = "true" ] && features+=("Speaker Diarization")
    [ "$WHISPER_PRINT_PROGRESS" = "true" ] && features+=("Progress Display")

    if [ ${#features[@]} -gt 0 ]; then
        log_info "   Features: ${features[*]}"
    fi

    echo
    log_info "🎙️  Server will be available at: http://$WHISPER_HOST:$WHISPER_PORT"
    log_info "📡 Health check endpoint: http://$WHISPER_HOST:$WHISPER_PORT/"
    echo

    log_info "⚡ Executing: ./whisper-server $server_args"
    echo
    echo -e "${BLUE}[2025-01-15 $(date +%H:%M:%S)] Starting Whisper.cpp server...${NC}"

    exec ./whisper-server $server_args
}

show_help() {
    cat << EOF
Whisper Server Docker Container

Usage: docker run [docker-options] whisper-server [COMMAND]

Commands:
  server          Start the Whisper server (default)
  bash            Start bash shell
  test            Run connectivity test
  models          List available models
  gpu-test        Test GPU detection
  help            Show this help

Environment Variables:
  WHISPER_MODEL          Model path (default: models/ggml-base.en.bin)
  WHISPER_HOST           Server host (default: 0.0.0.0)
  WHISPER_PORT           Server port (default: 8178)
  WHISPER_THREADS        Thread count (default: auto)
  WHISPER_USE_GPU        Enable GPU (default: true)
  WHISPER_LANGUAGE       Language code (default: en)
  WHISPER_TRANSLATE      Translate to English (default: false)
  WHISPER_DIARIZE        Enable diarization (default: false)
  WHISPER_PRINT_PROGRESS Show progress (default: true)
  WHISPER_DEBUG          Enable debug logging (default: false)

Examples:

  docker run -e WHISPER_MODEL=models/ggml-large-v3.bin whisper-server

  docker run -p 8178:8178 whisper-server

  docker run -v /path/to/models:/app/models whisper-server

EOF
}

test_gpu() {
    log_info "=== GPU Detection Test ==="
    local gpu_type
    gpu_type=$(detect_gpu)
    log_info "Detected GPU type: $gpu_type"

    if [ "$gpu_type" = "nvidia" ]; then
        log_info "NVIDIA GPU Details:"
        nvidia-smi
    fi

    log_info "=== System Information ==="
    log_info "CPU cores: $(nproc)"
    log_info "Memory: $(free -h | grep Mem | awk '{print $2}')"
    log_info "Architecture: $(uname -m)"
}

list_models() {
    log_info "=== Available Models ==="

    if [ -d "/app/models" ]; then
        log_info "Models in /app/models:"
        find /app/models -name "*.bin" -type f | sort | while read -r model; do
            local size=$(du -h "$model" | cut -f1)
            log_info "  $model ($size)"
        done
    else
        log_warn "No models directory found"
    fi

    if [ -d "/app/local_models" ]; then
        log_info "Models in /app/local_models:"
        find /app/local_models -name "*.bin" -type f | sort | while read -r model; do
            local size=$(du -h "$model" | cut -f1)
            log_info "  $model ($size)"
        done
    fi
}

test_connectivity() {
    log_info "=== Connectivity Test ==="

    log_info "Testing external connectivity..."
    if curl -s --connect-timeout 5 https://huggingface.co >/dev/null; then
        log_info "✓ External connectivity OK"
    else
        log_warn "✗ External connectivity failed"
    fi

    log_info "Testing DNS resolution..."
    if nslookup huggingface.co >/dev/null 2>&1; then
        log_info "✓ DNS resolution OK"
    else
        log_warn "✗ DNS resolution failed"
    fi
}

main() {
    local command="${1:-server}"

    case "$command" in
        "server")
            start_server
            ;;
        "bash")
            exec /bin/bash
            ;;
        "test")
            test_connectivity
            ;;
        "models")
            list_models
            ;;
        "gpu-test")
            test_gpu
            ;;
        "help"|"--help"|"-h")
            show_help
            ;;
        *)
            log_error "Unknown command: $command"
            show_help
            exit 1
            ;;
    esac
}

trap 'log_info "Received shutdown signal, stopping server..."; exit 0' SIGTERM SIGINT

main "$@"
