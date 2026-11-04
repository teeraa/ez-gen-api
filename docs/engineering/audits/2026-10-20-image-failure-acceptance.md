# Image failure diagnostics acceptance

Coordinator: /root. Task: 2026-10-20-image-failure. Source scope and frozen five-file hashes: [implementation evidence](2026-10-20-image-failure.md). Existing dirty user work preserved. Complete changed-tree manifest, git status and staged/unstaged diffs recorded in /private/tmp/ez-gen-image-failure-evidence; documentary report hash refreshed after actual gate output appended. No source edits after freeze.

Acceptance: PASS for diagnostic improvements. Security reviewer image_diagnostic_security independently reviewed the same hashes and ran disclosure/log sanitizer tests: PASS. Fresh independent-auditor image_failure_audit checked source, tests, manifest and actual author execution evidence: PASS, no confirmed blocking findings. It independently ran focused race tests and build successfully. Author full go build ./..., go vet ./..., go test -race ./...: PASS exit0; actual transcript in implementation report.

Original job failure cause remains UNKNOWN: supplied JSON omits upstream diagnostic code and parameter. No original-job retry, paid generation, migration, deployment or production log/database access. Existing failed jobs are not rewritten. Safe diagnostics apply to subsequent errors after updated server startup. Tests use provider fixtures; live request success is NOT_RUN. The code intentionally retains existing outward error code strings, preserves usage callbacks, and adds fixed safe messages and allowlisted metadata only.

Documentation human review remains pending. No additional implementation work is required for this diagnostic scope; confirming the original provider rejection requires its sanitized upstream log.
