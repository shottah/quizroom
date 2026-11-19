SHELL := /bin/bash
DC := docker compose
ENV_FILE ?= .env

.PHONY: up down logs token api-key _write-secret doctor types test

up:
	$(DC) up -d --build

down:
	$(DC) down

logs:
	$(DC) logs -f --tail=100

# Interactive: needs a terminal. The token is never echoed or passed as an argument.
token:
	claude setup-token
	@echo; echo "Copy the long token printed above. It starts with sk-ant- (not the short code from the browser)."
	@read -s -p "Paste the token: " T; echo; printf '%s' "$$T" | $(MAKE) --no-print-directory _write-secret NAME=CLAUDE_CODE_OAUTH_TOKEN LABEL=token
	@if [ -n "$$($(DC) ps -q runner 2>/dev/null)" ]; then $(DC) up -d runner >/dev/null 2>&1 && echo "Runner restarted with the new token."; fi

# Interactive: needs a terminal. The key is never echoed or passed as an argument.
# With a key set, Claude bills your API account instead of using the subscription.
api-key:
	@echo "Create a key at https://platform.claude.com/settings/keys (it starts with sk-ant-api)."
	@read -s -p "Paste the API key: " T; echo; printf '%s' "$$T" | $(MAKE) --no-print-directory _write-secret NAME=ANTHROPIC_API_KEY LABEL="API key"
	@if [ -n "$$($(DC) ps -q runner 2>/dev/null)" ]; then $(DC) up -d runner >/dev/null 2>&1 && echo "Runner restarted: Claude now uses the API key."; fi

# Reads a secret from stdin and stores it as $(NAME) in $(ENV_FILE) (mode 600).
# Refuses anything that isn't shaped like a Claude credential (sk-ant-..., 40+ chars).
_write-secret:
	@set -eu; \
	IFS= read -r T || true; \
	if [ -z "$$T" ]; then echo "No $(LABEL) received; nothing written." >&2; exit 1; fi; \
	case "$$T" in sk-ant-*) ;; *) echo "That doesn't look like a Claude $(LABEL) (it should start with sk-ant-). Nothing written." >&2; exit 1;; esac; \
	if [ $${#T} -lt 40 ]; then echo "That $(LABEL) looks cut off (only $${#T} characters). Nothing written. Copy the whole $(LABEL) and try again." >&2; exit 1; fi; \
	[ -f "$(ENV_FILE)" ] || cp .env.example "$(ENV_FILE)"; \
	TMP=$$(mktemp "$(ENV_FILE).XXXXXX"); \
	trap 'rm -f "$$TMP"' EXIT; \
	grep -v '^$(NAME)=' "$(ENV_FILE)" > "$$TMP" || true; \
	printf '$(NAME)=%s\n' "$$T" >> "$$TMP"; \
	chmod 600 "$$TMP"; \
	mv "$$TMP" "$(ENV_FILE)"; \
	trap - EXIT; \
	echo "Saved $(NAME) to $(ENV_FILE)."

doctor:
	@bash scripts/doctor.sh

# Regenerates the frontend's API types from the backend's Go types.
types:
	docker run --rm -v "$(CURDIR):/src" -v quizroom_gomod:/go/pkg/mod -w /src/backend golang:1.27-alpine \
		go run ./cmd/quizroom openapi > frontend/src/api/openapi.json.tmp
	mv frontend/src/api/openapi.json.tmp frontend/src/api/openapi.json
	docker run --rm -v "$(CURDIR)/frontend:/app" -v /app/node_modules -w /app node:26-alpine \
		sh -c "npm ci --no-audit --no-fund >/dev/null && npm run types"

# Go tests against a throwaway Postgres, then the frontend's test stage. Never calls Claude.
test:
	$(DC) --profile test run --rm backend-test; status=$$?; $(DC) --profile test rm -sf test-db >/dev/null 2>&1; exit $$status
	docker build --target test frontend
