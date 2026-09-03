#!/bin/bash

# Exit on error
set -e

# Optional log level argument. Without one, an already-exported RUST_LOG is
# kept, so `RUST_LOG=app_lib::audio=debug ./clean_run.sh` works; the app parses
# env-filter style directives (`level` or `target=level`, comma separated).
LOG_LEVEL=${1:-}

if [ -n "$LOG_LEVEL" ]; then
    case $LOG_LEVEL in
        info|debug|trace)
            export RUST_LOG=$LOG_LEVEL
            ;;
        *)
            echo "Invalid log level: $LOG_LEVEL. Valid options: info, debug, trace"
            exit 1
            ;;
    esac
elif [ -z "${RUST_LOG:-}" ]; then
    export RUST_LOG=info
fi

# Clean up previous builds
echo "Cleaning up previous builds..."
#rm -rf target/
#rm -rf src-tauri/target
#rm -rf src-tauri/gen

# Clean up npm, pnp and next
echo "Cleaning up npm, pnp and next..."
rm -rf node_modules
rm -rf .next
rm -rf .pnp.cjs
rm -rf out

echo "Installing dependencies..."
pnpm install

# Build the Next.js application first
echo "Building Next.js application..."
pnpm run build

# Set environment variables for the build
echo "Setting up build environment..."

echo "Building Tauri app..."
pnpm run tauri dev
sleep

