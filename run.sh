#!/bin/bash
# run.sh for song - Standalone song service
#
# Detects if song is already running locally and prompts:
#   use local / start new local / use remote
# Default: use any running local instance

set -e

SONG_PORT="${SONG_PORT:-8082}"

# Check if song is already running locally
is_local_running() {
    curl -s "http://127.0.0.1:$SONG_PORT/_song/health" > /dev/null 2>&1
    return $?
}

# Check if song remote is reachable
is_remote_running() {
    curl -s "http://${SONG_REMOTE:-127.0.0.1:$SONG_PORT}/_song/health" > /dev/null 2>&1
    return $?
}

echo "=== song Service ==="
echo "Configuration:"
echo "  Local address: 127.0.0.1:$SONG_PORT"

SELECTED=""

if is_local_running; then
    echo "✓ Local song is already running on 127.0.0.1:$SONG_PORT"
    read -p "Use local instance? [Y/n/remote]: " choice
    case "$choice" in
        n|N)
            SELECTED="remote"
            ;;
        *)
            SELECTED="local"
            ;;
    esac
fi

if [ "$SELECTED" = "" ]; then
    if is_remote_running; then
        echo "✓ Remote song is reachable at ${SONG_REMOTE:-127.0.0.1:$SONG_PORT}"
        read -p "Use remote instance? [y/N/local]: " choice
        case "$choice" in
            y|Y)
                SELECTED="remote"
                ;;
            l|L)
                SELECTED="local"
                ;;
            *)
                SELECTED="local"
                ;;
        esac
    else
        echo "✗ No local or remote song running. Starting new local instance..."
        SELECTED="local"
    fi
fi

case "$SELECTED" in
    local)
        echo ">>> Starting new local song instance..."
        if [ -x "$(command -v song)" ]; then
            song -port "$SONG_PORT" -root "./data/song" &
            echo "    PID: $!"
            echo "✓ song started locally on 127.0.0.1:$SONG_PORT"
        else
            echo "✗ song binary not found. Please build first: go build ./..."
        fi
        ;;
    remote)
        echo ">>> Using remote song at ${SONG_REMOTE:-127.0.0.1:$SONG_PORT}"
        if [ -z "$SONG_REMOTE" ]; then
            export SONG_REMOTE="${SONG_REMOTE:-127.0.0.1:$SONG_PORT}"
            echo "✓ Remote set: $SONG_REMOTE"
        fi
        ;;
    *)
        echo ">>> Starting new local song instance..."
        if [ -x "$(command -v song)" ]; then
            song -port "$SONG_PORT" -root "./data/song" &
            echo "    PID: $!"
            echo "✓ song started locally on 127.0.0.1:$SONG_PORT"
        else
            echo "✗ song binary not found. Please build first: go build ./..."
        fi
        ;;
esac

echo ""
echo "song service ready on port $SONG_PORT"
echo "API endpoints:"
echo "  Health:     http://127.0.0.1:$SONG_PORT/_song/health"
echo "  Silos:      http://127.0.0.1:$SONG_PORT/_song/silos"
echo "  Components: http://127.0.0.1:$SONG_PORT/_song/components"
echo "  Static:     http://127.0.0.1:$SONG_PORT/_song/"
