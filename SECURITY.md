# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub: on the repository's **Security** tab, choose **Report a vulnerability**. Don't open a public issue.

Include what you found, how to reproduce it, and what an attacker could do with it. Quizroom is a small project, so replies are best effort. Only the latest `main` gets security fixes.

## How Quizroom is meant to run

Quizroom is a single-user app for your own computer. What it does and doesn't protect:

- **There's no login.** Anyone who can reach port 8790 can use your copy, read your progress and spend your Claude usage. Compose publishes the port on 127.0.0.1 only. Exposing it to a network or the internet without your own authentication in front isn't supported.
- **Other websites are refused.** The API answers only requests addressed to localhost, 127.0.0.1 or a name in `ALLOWED_HOSTS`, which stops DNS rebinding. It also refuses browser requests that another site sent, judged by the `Sec-Fetch-Site` and `Origin` headers.
- **The Claude credential stays in the runner.** It's stored in `.env` (mode 600) and passed only to the `runner` container, which only the backend can reach.
- **Claude runs without tools.** The runner starts the Claude Code CLI as `claude -p` with no tools, MCP servers, settings or saved sessions. With an API key, each call is capped at $1.
- **Claude's output is untrusted.** The frontend renders it without raw HTML.

In scope, for example: the credential reaching the browser, the logs, the backend or the database; Claude-written content running script in the page; a way to make the runner use tools or run commands; a web page you visit being able to drive your local Quizroom.
