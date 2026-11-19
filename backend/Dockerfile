# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/quizroom ./cmd/quizroom

# The API: one static binary on distroless, running as nonroot.
FROM gcr.io/distroless/static-debian13:nonroot AS backend
COPY --from=build /out/quizroom /quizroom
EXPOSE 8080
ENTRYPOINT ["/quizroom"]
CMD ["serve"]

# The Claude CLI ships as one native binary inside its npm package. Install it
# here, then copy only that binary into the runner, so Node isn't shipped.
FROM node:26-slim AS claude
RUN npm i -g @anthropic-ai/claude-code@2.1.327 \
    && cp "$(readlink -f /usr/local/bin/claude)" /claude \
    && /claude --version

# The Claude runner: the pinned Claude Code CLI plus the same binary in runner mode.
FROM debian:trixie-slim AS runner
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=claude /claude /usr/local/bin/claude
RUN useradd --uid 1001 --create-home --home-dir /home/runner runner
COPY --from=build /out/quizroom /usr/local/bin/quizroom
ENV DISABLE_UPDATES=1 DISABLE_AUTOUPDATER=1 HOME=/home/runner
USER runner
WORKDIR /home/runner
EXPOSE 9000
ENTRYPOINT ["quizroom"]
CMD ["runner"]
