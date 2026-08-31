# RelayShelf Codex workflow

1. For ordinary bug fixes and UI polish, create commits only. Do not create a Git tag, GitHub Release, or production deployment unless the user explicitly requests it.
2. After UI or UX changes, run only the tests that are directly relevant to the changed files or behavior, prefer starting `make dev-preview`, report port 5173 and the test accounts, and give the user focused manual acceptance steps.
3. Local validation follows the minimum-sufficient rule: inspect `git diff --name-only` first, then choose the narrowest useful test scope. Do not run broad or full test suites by habit.
4. Do not run `make dev-check`, `make test`, `make e2e`, `go test ./...`, the full frontend Vitest suite, or the full integration suite by default. Full regression belongs to GitHub CI.
5. For frontend-only changes, prefer the directly related Vitest file(s), targeted typecheck/lint when needed, and Dev Preview. Pure CSS/layout/text/icon changes may rely primarily on Dev Preview plus a targeted browser test only when the changed behavior requires it.
6. For Go changes, test the affected package first, for example `go test ./internal/messages/...` or `go test ./internal/files/...`. Run package-scoped integration tests only when the change touches database transactions, repositories, authorization boundaries, upload/file behavior, concurrency, or similar integration semantics.
7. For OpenAPI changes, run `make generate`, inspect the generated diff, and test only the backend/frontend areas affected by the contract change. Do not run full E2E solely because OpenAPI changed.
8. Run local E2E only for changed browser journeys or browser-specific behavior such as navigation, selectors, login, upload/download flows, responsive behavior, or when the user explicitly asks for it. Prefer a single Playwright spec or focused case over the entire suite.
9. Full local testing is appropriate only when the change spans multiple core modules and the impact cannot be bounded reasonably, when test infrastructure/global generation is being changed, when CI is unavailable, when debugging a cross-module failure, or when the user explicitly requests a broad local check.
10. When reporting completion, state exactly what local tests were run and what broad suites were intentionally not run. Example: `MessageDetailView.test.ts PASS; full frontend tests/E2E not run because the change is isolated to Message Inspector UI; full regression is delegated to CI.`
11. GitHub CI remains the authority for full regression before release. A local targeted PASS means focused validation passed; only green CI means full regression passed.
12. The Dev environment may use only `.local/dev` and its independent `relayshelf_dev` PostgreSQL resources. Never use a production database, NFS mount, `.env`, or production secrets for Dev Preview.
13. Multiple small fixes may accumulate on `main` for user acceptance before a single release. Full CI is still required before release.
14. Explicit user instructions to publish, tag, release, deploy, or run broader tests override these defaults.
