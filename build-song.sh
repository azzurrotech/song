#!/bin/bash
# build-song.sh - Build and run the SONG (static hosting/Web Components) service
#
# Usage:
#   ./build-song.sh           # build and start on default port
#   ./build-song.sh 8082    # start on port 8082
#   ./build-song.sh http://localhost:8082  # use existing running service
#
# Automatically detects an open local port if none specified,
# otherwise accepts a URL as argument.

set -e

# Defaults
PORT=""
ROOT_DIR="./data"

# Parse arguments: if a port number or URL is given, use it
if [ $# -ge 1 ]; then
  FIRST="$1"
  # Check if it looks like a URL
  if echo "$FIRST" | grep -q '^http://\|^https://'; then
    PORT="$FIRST"
  else
    # Assume it's a port number
    if ! echo "$FIRST" | grep -qE '^[0-9]+$'; then
      echo "ERROR: Expected a port number or URL, got: $FIRST"
      exit 1
    fi
    PORT=":$FIRST"
  fi
fi

# Find an open port if not provided
if [ -z "$PORT" ]; then
  for trial_port in 8082 8083 8084 8085 9090 3000; do
    if ! curl -s "http://localhost${trial_port}" > /dev/null 2>&1; then
      PORT=":$trial_port"
      echo ">>> Using available port $trial_port"
      break
    fi
  done
  if [ -z "$PORT" ]; then
    echo ">>> No default port available, relying on Go defaults/env"
    PORT=""
  fi
fi

# Build the Go binary
echo ">>> Building SONG binary..."
go build -o song .

echo ">>> Starting SONG..."
if [ -n "$PORT" ]; then
  echo ">>> Using port $PORT"
  export SONG_PORT="${PORT#:}"
  export SONG_ROOT="$ROOT_DIR"
  ./song -port "${PORT#:}" -root "$ROOT_DIR"
else
  ./song -port ":8080" -root "$ROOT_DIR"
fi