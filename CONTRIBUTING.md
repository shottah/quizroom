# Contributing to Quizroom

Thanks for helping. Quizroom is a small project, so for anything bigger than a bug fix, please open an issue first and agree on the approach before you write the code.

## Set up

You need Docker with Compose v2 and `make`. Everything builds and runs in containers, so Go 1.27 and Node 26 on your own computer are optional; they only help your editor.

```sh
make up      # build and start the stack on http://127.0.0.1:8790
make logs    # follow the logs
make doctor  # check the containers, the app and the Claude credential
```

Writing questions and the chat need a Claude credential (`make token` or `make api-key`; see the [README](README.md#which-claude-credential)). Everything else works without one, including the whole test suite.

## Test

```sh
make test
```

This runs the Go tests against a throwaway Postgres, then the frontend's typecheck, its unit tests (Vitest) and a guard against raw HTML. It never calls Claude: the chat tests use a fake runner. CI runs the same command on every pull request, and also checks that the generated API files are up to date.

To run some Go tests on their own:

```sh
docker compose --profile test run --rm backend-test go test ./internal/quiz -run TestCheck
docker compose --profile test rm -sf test-db
```

Go tests that need the database skip themselves when `TEST_DATABASE_URL` is unset, so a plain `go test ./...` on your computer runs only the others.

## Conventions

- **Test first.** A behaviour change comes with a test that failed before the change. Go tests run against a real Postgres rather than mocks.
- **Formats are defined once, in Go.** Request and response types live in the backend. After changing one, run `make types` and commit the regenerated `frontend/src/api/openapi.json` and `frontend/src/api/schema.d.ts`. `TestOpenAPIMatchesFrontendCopy` fails if you forget.
- **Database changes are new migrations.** Add a numbered file to `backend/internal/store/migrations/`. Migrations run in order at startup and are recorded by number, so never edit or renumber one that has shipped.
- **The rating maths has a simulation.** `backend/internal/sim` runs simulated learners through the real rating and picking code. Changes there must keep it passing: it checks that ratings move gradually and that the ± margin is honest.
- **Claude's text is untrusted.** Render it with the `RichText` component, never with `dangerouslySetInnerHTML`; `make test` fails if that appears.
- **The runner stays locked down.** Claude runs as `claude -p` with no tools, MCP servers or settings, and the credential never leaves the runner container. Changes there need a clear reason.
- **Plain copy.** UI text uses plain words, and every number says what it means ("about a 2 in 3 chance your real level is inside the margin").
- **Formatting.** `make test` fails on Go code that `gofmt` would change. TypeScript is compiled in strict mode.

## Pull requests

- Keep each pull request to one change, and say why it's needed.
- Use a conventional-commit title, such as `fix(quiz): keep the practice reserve during a skill check`.
- Say how you tested it, and add screenshots for UI changes.

By contributing, you agree that your contributions are licensed under the project's license, the GNU AGPL version 3 or later.

## Security issues

Please don't report vulnerabilities in public issues. See [SECURITY.md](SECURITY.md).
