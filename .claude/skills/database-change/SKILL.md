# Skill: database-change

Add or modify a database schema — new table, new column, new index, or GORM model update.

## When to Use

- Adding a new table
- Adding a column to an existing table
- Adding or removing an index
- Updating a GORM model struct to match a schema change
- Reviewing whether a schema change is safe before implementation

---

## Pre-Flight: Read First

```
[ ] docs/engineering/video-data-model-proposal.md — approved schema
[ ] CLAUDE.md — rule: no AutoMigrate in production
[ ] .claude/agents/database-reviewer.md — full checklist
[ ] The relevant GORM model file (if it exists)
[ ] The latest migration file (to get the correct next migration number)
```

---

## Step 1: Check the Proposal

Confirm the change is in `docs/engineering/video-data-model-proposal.md`.

If not:
1. Update the proposal with the new table/column design.
2. Get coordinator review.
3. Then write the migration.

Do not write a migration for a schema not in the approved proposal.

---

## Step 2: Determine Next Migration Number

```bash
ls migrations/ | sort -n | tail -1
# If last is 005_create_model_configs.sql, next is 006_...
```

---

## Step 3: Write the Migration File

`migrations/<NNN>_<description>.sql`:

```sql
-- Migration: <NNN>_<description>
-- Description: <one-line description>
-- Depends on: <previous migration>
-- Reversible: yes / no

BEGIN;

-- Your DDL here
CREATE TABLE example (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

-- Indexes
CREATE INDEX idx_example_user_id ON example(user_id) WHERE deleted_at IS NULL;

COMMIT;
```

### Required Patterns

| Requirement | Pattern |
|---|---|
| UUID PK | `id UUID PRIMARY KEY DEFAULT gen_random_uuid()` |
| Timestamp | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` |
| Soft delete | `deleted_at TIMESTAMPTZ` |
| FK index | `CREATE INDEX idx_<table>_<fk_col> ON <table>(<fk_col>)` |
| Adding column to live table | `ALTER TABLE ... ADD COLUMN ... DEFAULT ...` |
| Index on live table | `CREATE INDEX CONCURRENTLY ...` (outside transaction!) |

### Adding a Column Safely

```sql
-- Safe: nullable column, no default required
ALTER TABLE video_generation_jobs ADD COLUMN new_field TEXT;

-- Safe: NOT NULL with DEFAULT
ALTER TABLE video_generation_jobs ADD COLUMN retry_count INT NOT NULL DEFAULT 0;

-- UNSAFE on large table — verify row count before running:
-- ALTER TABLE ... ADD COLUMN ... NOT NULL;  (without default)
```

---

## Step 4: Update the GORM Model

`apps/models/<table>.go`:

```go
type VideoGenerationJob struct {
    ID            uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    UserID        uuid.UUID      `gorm:"type:uuid;not null"`
    Status        string         `gorm:"not null;default:queued"`
    // ... other fields

    // Soft delete
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (VideoGenerationJob) TableName() string { return "video_generation_jobs" }
```

GORM model must match the SQL schema exactly:
- Column names match (Go uses PascalCase, SQL uses snake_case — GORM converts automatically)
- Nullability matches: pointer type = nullable, value type = NOT NULL
- Defaults declared in both SQL and GORM tag
- `TableName()` override when the default GORM pluralization is wrong

---

## Step 5: Database Reviewer Sign-off

Run the `database-reviewer` agent on your migration file and GORM model before proceeding.

Checklist it must pass:
```
[ ] All timestamps are TIMESTAMPTZ
[ ] All PKs use gen_random_uuid()
[ ] NOT NULL columns with no DEFAULT are only on new tables (not ALTER)
[ ] Every FK column has an index
[ ] Filtered indexes include WHERE deleted_at IS NULL for soft-deleted tables
[ ] GORM model nullability matches SQL nullability
[ ] Migration is wrapped in BEGIN/COMMIT
[ ] Migration file number is sequential
```

---

## Step 6: Verify Locally

```bash
# Apply migration to local test database (NOT production)
psql $DATABASE_URL < migrations/<NNN>_<description>.sql

# Verify schema
psql $DATABASE_URL -c "\d <table_name>"

# Run GORM model test to confirm it maps correctly
go test -run TestModel<Name> ./apps/models/...
```

---

## Step 7: Integration Test

Write a basic integration test that:
1. Creates a row
2. Reads it back
3. Updates it
4. Soft-deletes it (verify it doesn't appear in normal queries)
5. Hard-verifies the deleted row exists with `Unscoped()`

---

## What You Must NOT Do

- Never run `gorm.AutoMigrate()` outside of a test file.
- Never edit an already-applied migration file.
- Never apply a migration to the production database directly — use the CI/CD migration runner.
- Never add a NOT NULL column without a DEFAULT to a table that already has rows.
- Never drop a column without first confirming no code references it.
