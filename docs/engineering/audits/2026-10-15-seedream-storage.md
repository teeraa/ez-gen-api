# Seedream 4.5 and private character bucket

## Implementation and independent review: PASS

User scope: connect character generation to Seedream `seedream-4-5-251128` and separate Supabase image storage from video. Generic implementation and independent-auditor agents applied the repository roles; these were delegated agents, not registered custom-role invocations.

The image model has independent server configuration and optional image API credentials/endpoint with shared credentials as fallback. New character keys use `characters/` routed to `STORAGE_IMAGE_BUCKET`; legacy objects remain in `STORAGE_BUCKET`. All existing readers use the same router. No frontend contract change or new migration is required. Existing database migrations through 015 were confirmed read-only.

Local `.env` was updated only for `ARK_IMAGE_MODEL=seedream-4-5-251128` and `STORAGE_IMAGE_BUCKET=violet-characters`; credentials/video settings were preserved and excluded from evidence. The explicit provisioning command `go run ./cmd/setup-character-storage` completed successfully against configured Supabase. It creates a missing private PNG/JPEG bucket with a 24 MiB limit and refuses incompatible existing buckets without modifying them.

Author gates: `go build ./...`, `go vet ./...`, `go test -race ./...` PASS. [Test output](2026-10-15-seedream-storage-tests.log). Independent reviewer ran configs/storage/crons/modelark/provisioning tests successfully and bound all 14 files in the [manifest](2026-10-15-seedream-storage-snapshot.json). No blocking findings; `git diff --check` passed. Earlier sandbox fixture-bind/cache failures were resolved with permitted execution, not application changes.

Runtime: no queued image jobs were found before restart. Only identified API8080 Go processes in this repository were restarted with current configuration. A short-lived in-memory diagnostic session fetched image-capabilities: HTTP200 and enabled=true. No account/database writes or AI generation requests were made by this diagnostic check.

Live Supabase storage smoke check: PASS. Uploaded a generated 2×2 diagnostic PNG to the private character bucket, fetched the identical bytes through a signed URL, and deleted only that diagnostic object (cleanup HTTP200). No user objects were modified and no AI model was called.

## Verification limits

Actual Seedream generation/account activation and billed output remain NOT_RUN; no provider credits were spent. Successful capabilities only indicates server configuration, not provider activation. User-submitted creation will invoke the configured real model. No deployment readiness is claimed. Private bucket settings and environment are operational prerequisites for other environments.

Official references: [BytePlus Seedream](https://docs.byteplus.com/en/docs/modelark/seedream-4-0-5-0), [Supabase private buckets](https://supabase.com/docs/guides/storage/buckets/fundamentals).
