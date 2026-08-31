# RelayShelf Codex workflow

1. For ordinary bug fixes and UI polish, create commits only. Do not create a Git tag, GitHub Release, or production deployment unless the user explicitly requests it.
2. After UI or UX changes, run the relevant tests, prefer starting `make dev-preview`, report port 5173 and the test accounts, and give the user focused manual acceptance steps.
3. The Dev environment may use only `.local/dev` and its independent `relayshelf_dev` PostgreSQL resources. Never use a production database, NFS mount, `.env`, or production secrets for Dev Preview.
4. Multiple small fixes may accumulate on `main` for user acceptance before a single release. Full CI is still required before release.
5. Explicit user instructions to publish, tag, release, or deploy override the default no-release rule.
