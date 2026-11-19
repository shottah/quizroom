#!/usr/bin/env bash
# Checks the stack: containers, the app, and which Claude credential the runner has.
set -u
cd "$(dirname "$0")/.."

echo "== Containers"
docker compose ps --format 'table {{.Service}}\t{{.State}}\t{{.Status}}' 2>/dev/null || echo "docker compose isn't available"
echo

echo "== App"
if ! status=$(curl -fsS --max-time 8 http://127.0.0.1:8790/api/status 2>/dev/null); then
  echo "The app isn't answering on http://127.0.0.1:8790. Run make up, then make logs."
  exit 1
fi
field() { printf '%s' "$status" | python3 -c "import json, sys; print(json.load(sys.stdin).get('$1', ''))"; }
auth=$(field auth)
last=$(field last_error)
echo "Claude: ${auth} ($(field claude))"
echo "Topics: $(field topics), questions: $(field questions)"
case "$auth" in
  none) echo "Run make token to sign the runner in with your Claude subscription." ;;
  unreachable)
    echo "The runner isn't reachable. Its last log lines:"
    docker compose logs --tail=20 runner
    ;;
  api_key) echo "Claude calls are billed to your API account. Remove ANTHROPIC_API_KEY and run make up to use your subscription." ;;
  subscription) [ -z "$last" ] && echo "Ready: Claude uses your subscription." ;;
esac
if [ -n "$last" ]; then
  echo "The last Claude job failed: $last"
fi
