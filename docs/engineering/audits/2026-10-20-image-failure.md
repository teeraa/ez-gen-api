# Image failure diagnostics — 2026-10-20

Task ID: 2026-10-20-image-failure. Author: modelark-specialist `/root/image_failure_diagnostics`; coordinator `/root` assigns independent review. Working directory: `/Users/mac/Workspace/ez-gen/ez-gen-api`.

## Scope and acceptance

Investigate reported job `e9a0f140-a6c8-4beb-a9df-8d055cb33b1e`; improve actionable errors without guessing its cause, changing API error code strings, exposing provider payloads, changing generation parameters, or making paid calls. Allowed/actual files: `apps/services/modelark/image_client.go`, `image_errors.go`, `image_errors_test.go`, `apps/crons/image_worker.go`, `image_worker_test.go`, and this report. All other files prohibited. Existing dirty work, including usage tracking, preserved.

Acceptance: fixed allowlists for diagnostic code families/parameters; safe Thai messages; diagnostics at worker with job/model/size/reference-presence; handle HTTP and HTTP200 embedded errors; fake-server proof of single attempt and no secret disclosure; build/vet/race gates; fresh independent audit required before acceptance.

## Confirmed findings and limits

F1 MEDIUM: prior `classifyImageError` discarded HTTP400 provider code/parameter and returned the same generic message. Thus the supplied API response cannot establish whether prompt, reference access, model settings, or moderation caused this failure. Do not assert Spider-Man caused moderation. Resolved by allowlisted family/parameter diagnostics and contextual messages, preserving established API codes.

F2 MEDIUM: `GenerateImage` discarded error details in HTTP200 top-level `error` or `data[].error`, classifying them as invalid output. Resolved by routing both through sanitized classification. Provider messages, raw errors, request IDs, prompts and reference URLs never enter diagnostic logs.

Current worker signs owned reference images for ten minutes; actual reference availability, dimensions and provider rejection code for the reported job remain UNKNOWN. Existing failed job is not modified or retried. No production logs/database or secrets inspected.

Official BytePlus [image API](https://docs.byteplus.com/en/docs/modelark/image-generation-api?redirect=1), retrieved 2026-10-20 through its [official markdown mirror](https://ai.byteplus.com/ark/region:ap-southeast-1/docs/image-generation-api.md), saved `/tmp/image-generation-api.md`. It documents accessible URL or data-URL reference input; image dimensions >14 pixels, ratio 1/16..16, <=30MB, <=36 million pixels. Seedream5 lite output pixels 3,686,400..16,777,216 and ratio 1/16..16. All current worker output sizes satisfy those limits, including 1728x2304 explicitly listed for 3:4. `b64_json` is supported. No evidence justifies changing size or response format. Actual SDK import `github.com/volcengine/ark-runtime-go v0.11.0` checked against local module source and fake HTTP serialization.

## Feature map and verification

- Safe code/parameter/error -> `image_errors.go` -> `TestImageDiagnosticsOmitProviderSecrets`: PASS.
- SDK HTTP and embedded errors -> `image_client.go` -> `TestImageClientRetainsSafeHTTPAndEmbeddedDiagnostics`: PASS; exactly one call.
- Snapshot log fields -> `image_worker.go` -> `TestImageLogValuesRejectControlsAndURLs`: PASS.
- Existing no-retry/reference serialization/durable persistence/usage behavior -> existing modelark and crons tests: PASS.

Author checks, 2026-10-20 09:35–09:36 UTC, directory above:

- `go test ./apps/services/modelark ./apps/crons`: PASS exit0 (modelark 0.706s, crons 1.270s). Initial restricted-cache run failed due sandbox cache permissions; rerun with approved escalation passed.
- `go build ./...`: PASS exit0.
- `go vet ./...`: PASS exit0, no warnings.
- `go test -race ./...`: PASS exit0; final apps/tests 6.088s, configs 4.363s; all packages passed.
- Paid/live provider request and original-job reproduction: NOT_RUN (not authorized; no real rejection evidence).
- Independent audit/security review: pending coordinator evidence; author does not self-accept.
- Documentation human review: pending.

## Frozen target

Base/head: `4e5a68734097bc8e3f1c31c1fcce392018d432f6` (no commit). Extensive pre-existing dirty tree; five source/test files below were untracked before this task. Source freeze 2026-10-20T09:35:23Z. This report excluded from its own manifest; coordinator owns complete dirty-tree/staged/unstaged evidence collection.

```text
a4aa3ad8d37641e0d49a5a7a1f7ba67300c0cba1a01508c6693773332cf9ed32  apps/services/modelark/image_client.go
6ead35f2a3cddee8d7c6e9f64ba35382602272b60eb266212df7b72a1495b158  apps/services/modelark/image_errors.go
714633aa6e301fe65b819f02774a3268faffda3167802a93bb284a4f28e385fa  apps/services/modelark/image_errors_test.go
692a40156eb212da08311681fd882ba02d7f5a1949a0f9ec3a3256f3583ebb83  apps/crons/image_worker.go
a515e6427a755caa1f6bae21ea4461f9f04814e10e78e8296672425a8818d417  apps/crons/image_worker_test.go
```

Verdict: BLOCKED pending independent acceptance; original failure cause unresolved pending sanitized upstream status/code/param. No commit, migration, deployment or paid provider call performed.

## Author execution transcript

The following is copied from tool results, rather than a separately rerun check. All commands used the working directory above. Commands started approximately 2026-10-20T09:35:40Z; final race result was retrieved after 09:35:55Z. Tool IDs are author-thread execution identifiers, not independent verification.

`go build ./...`: initial chunk `f0086d` returned session `36671`; final poll chunk `8e472a` returned `exit_code: 0`, `output: ""`.

`go vet ./...`: chunk `cd4099` returned `exit_code: 0`, `output: ""`.

`go test -race ./...`: initial chunk `546b75` returned session `64026`. Poll chunk `8e6240` returned the following output while still running:

```text
?   	github.com/ez-gen/ez-gen-api	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/crons	1.943s
?   	github.com/ez-gen/ez-gen-api/apps/db	[no test files]
?   	github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api	1.918s
ok  	github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api	3.781s
?   	github.com/ez-gen/ez-gen-api/apps/models	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api	(cached)
?   	github.com/ez-gen/ez-gen-api/apps/requests/request_open_api	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/services/characterdesign	(cached)
ok  	github.com/ez-gen/ez-gen-api/apps/services/modelark	3.822s
ok  	github.com/ez-gen/ez-gen-api/apps/services/productdesign	(cached)
?   	github.com/ez-gen/ez-gen-api/apps/services/promptcompiler	[no test files]
ok  	github.com/ez-gen/ez-gen-api/apps/services/storage	(cached)
ok  	github.com/ez-gen/ez-gen-api/apps/services/videoprocessing	(cached)
```

Final poll chunk `f29f7d` returned `exit_code: 0` and:

```text
ok  	github.com/ez-gen/ez-gen-api/apps/tests	6.088s
?   	github.com/ez-gen/ez-gen-api/apps/utils	[no test files]
ok  	github.com/ez-gen/ez-gen-api/cmd/setup-character-storage	(cached)
ok  	github.com/ez-gen/ez-gen-api/middlewares	(cached)
?   	github.com/ez-gen/ez-gen-api/migrate	[no test files]
ok  	github.com/ez-gen/ez-gen-api/pkgs/configs	4.363s
```

Task baseline evidence limit: no complete pre-edit file snapshot was saved. The initial author tool results contain full pre-edit image client/worker/tests, and `/tmp/image-error-original.txt` contains the exact pre-edit ImageError/classifier block. Broad substring checks for quota/balance/model are inherited behavior. The scoped changes extract that block, add allowlisted diagnostics/messages, classify embedded image errors, append worker diagnostic logging/helper and append sanitizer coverage. The coordinator records the complete final dirty-tree manifest and diffs; that artifact does not retroactively establish pre-task state.
