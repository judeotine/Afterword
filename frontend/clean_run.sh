#!/bin/bash

set -e

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

echo "Cleaning up previous builds..."

echo "Cleaning up npm, pnp and next..."
rm -rf node_modules
rm -rf .next
rm -rf .pnp.cjs
rm -rf out

echo "Installing dependencies..."
pnpm install

echo "Building Next.js application..."
pnpm run build

echo "Setting up build environment..."

echo "Building Tauri app..."
pnpm run tauri dev
sleep
