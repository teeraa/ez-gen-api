# AI usage — coordinator verification

Scope: frozen manifest in 2026-10-20-ai-usage.md. No source changes after freeze.

Command: `GOCACHE=/private/tmp/ez-gen-ai-usage-go-cache go build ./... && GOCACHE=/private/tmp/ez-gen-ai-usage-go-cache go vet ./... && GOCACHE=/private/tmp/ez-gen-ai-usage-go-cache go test -race ./...`

Working directory: /Users/mac/Workspace/ez-gen/ez-gen-api. Executed 2026-10-20. Final exit code: 0. Build and vet produced no output; the chained command reached tests and passed.

```text
?   	github.com/ez-gen/ez-gen-api	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/crons	2.425s
?   	github.com/ez-gen/ez-gen-api/apps/db	[no test files]
?   	github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api	2.503s
ok  	github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api	4.519s
?   	github.com/ez-gen/ez-gen-api/apps/models	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api	5.917s
?   	github.com/ez-gen/ez-gen-api/apps/requests/request_open_api	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/services/characterdesign	5.121s
ok  	github.com/ez-gen/ez-gen-api/apps/services/modelark	7.651s
ok  	github.com/ez-gen/ez-gen-api/apps/services/productdesign	4.383s
?   	github.com/ez-gen/ez-gen-api/apps/services/promptcompiler	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/services/storage	6.737s
ok  	github.com/ez-gen/ez-gen-api/apps/services/videoprocessing	11.407s
ok  	github.com/ez-gen/ez-gen-api/apps/tests	11.077s
?   	github.com/ez-gen/ez-gen-api/apps/utils	[no test files]
ok  	github.com/ez-gen/ez-gen-api/cmd/setup-character-storage	8.788s
ok  	github.com/ez-gen/ez-gen-api/middlewares	9.604s
?   	github.com/ez-gen/ez-gen-api/migrate	[no test files]
ok  	github.com/ez-gen/ez-gen-api/pkgs/configs	10.642s
```

Independent isolated PostgreSQL verification: disposable cluster /private/tmp/ez-gen-usage-pg.OSWx1e, Unix socket only, TCP disabled, test user usage_test, minimal parent users/jobs tables. Migration017 output (exit0): CREATE TABLE (parents), BEGIN, CREATE TABLE, CREATE INDEX x3, COMMIT. Stub jobs.deleted_at subsequently added for GORM scope. This validates the new migration against minimal referenced schema, not a full production upgrade.

Command: `AI_USAGE_TEST_DATABASE_URL='host=/private/tmp/ez-gen-usage-pg.OSWx1e port=55439 user=usage_test dbname=postgres sslmode=disable' GOCACHE=/private/tmp/ez-gen-ai-usage-go-cache go test -race -count=1 ./apps/repositories/repository_open_api -run '^TestAIUsagePersistence$' -v`

```text
=== RUN   TestAIUsagePersistence
--- PASS: TestAIUsagePersistence (0.05s)
PASS
ok github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api 1.805s
```

Final exit code 0. Temporary PostgreSQL server stopped successfully; production databases untouched. Full-suite database integration requires explicit opt-in and was independently run above.

Fresh independent auditor usage_independent_audit: PASS, no confirmed blocking findings. Independently checked all 11 source hashes, ran focused provider/repository tests and provider race tests, and inspected the full-gate and isolated PostgreSQL evidence above. Human documentation review pending. Deployment prerequisite: apply migration017 before updated application startup.

