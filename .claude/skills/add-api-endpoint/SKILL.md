# Skill: add-api-endpoint

Add a new API endpoint to the VIOLET STUDIO Go backend following the established layered architecture.

## When to Use

Use this skill when:
- Adding a new route to `/open/v1/`
- Adding a new route to `/api/v1/`
- Implementing a new operation in an existing domain (e.g., new job action)

Do NOT use this skill for:
- Schema changes (use `/database-change` first)
- ModelArk provider changes (use `/integrate-modelark`)
- Modifying existing endpoints without coordinator approval

---

## Pre-Flight Checklist

Before writing any code:

```
[ ] Read docs/engineering/video-generation-api-contract.md — locate the endpoint spec
[ ] Read docs/engineering/backend-architecture.md — confirm layer placement
[ ] Read CLAUDE.md — confirm rules
[ ] Read .claude/rules/go-conventions.md
[ ] Read .claude/rules/api-contracts.md
[ ] Read .claude/rules/security.md
[ ] Identify an existing similar endpoint and read its handler, facade, and test files
[ ] Confirm task scope with coordinator — get the allowed-files list
```

---

## Step 1: Request Struct

File: `apps/requests/request_<domain>/create_<action>.go`

```go
package request_<domain>

type Create<Action>Request struct {
    Field string `json:"field" validate:"required,min=1,max=200"`
    // Add all fields with validate: tags
}
```

Rules:
- All fields have `validate:` tags
- Enum fields use `oneof=val1 val2`
- Optional fields use `omitempty`
- Never put business logic here

---

## Step 2: DTO (Response Shape)

File: `apps/dto/dto_<domain>/<action>_response.go`

```go
package dto_<domain>

type <Action>Response struct {
    ID        string `json:"id"`
    Status    string `json:"status"`
    CreatedAt string `json:"createdAt"`
    // Match the field names in video-generation-api-contract.md exactly
}
```

Rules:
- No GORM tags
- No database-specific types
- No provider-specific fields
- All timestamps as ISO 8601 strings (not `time.Time`)
- Field names match `video-generation-api-contract.md` camelCase exactly

---

## Step 3: Repository Method

File: `apps/repositories/repository_<domain>/interface.go` + `postgres.go`

Add to the interface first:
```go
type <Domain>Repository interface {
    // existing methods...
    Create<Entity>(ctx context.Context, entity *models.<Entity>) error
}
```

Then implement in `postgres.go`:
```go
func (r *postgresRepo) Create<Entity>(ctx context.Context, entity *models.<Entity>) error {
    result := r.db.WithContext(ctx).Create(entity)
    if result.Error != nil {
        return fmt.Errorf("create <entity>: %w", result.Error)
    }
    return nil
}
```

Rules:
- Return wrapped errors, not raw GORM errors
- Use `ctx` in all DB calls
- Test with the `database-reviewer` agent if schema is involved

---

## Step 4: Facade Method

File: `apps/facades/facade_<domain>/interface.go` + `facade.go`

```go
// interface.go
type <Domain>Facade interface {
    Create<Action>(ctx context.Context, userID string, req request_<domain>.Create<Action>Request) (*dto_<domain>.<Action>Response, error)
}

// facade.go
func (f *facade) Create<Action>(ctx context.Context, userID string, req request_<domain>.Create<Action>Request) (*dto_<domain>.<Action>Response, error) {
    // 1. Domain validation (beyond field-level)
    // 2. Check ownership / permissions
    // 3. Call repository
    // 4. Call service if needed
    // 5. Map to DTO
    // 6. Return DTO (never return raw model or provider struct)
}
```

Rules:
- Facades must not import handler packages
- Return domain error sentinels (`ErrNotFound`, `ErrConflict`)
- Return DTOs, never GORM models or provider responses

---

## Step 5: Handler Method

File: `apps/handlers/handler_<domain>/handler.go`

```go
func (h *Handler) Create<Action>(c *fiber.Ctx) error {
    userID := c.Locals("userID").(string)
    rid := c.Locals("requestID").(string)

    var req request_<domain>.Create<Action>Request
    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(response.Error("VALIDATION_ERROR", "Invalid request body", nil, rid))
    }
    if errs := h.validator.Validate(req); errs != nil {
        return c.Status(400).JSON(response.ValidationError(errs, rid))
    }

    result, err := h.facade.Create<Action>(c.Context(), userID, req)
    if err != nil {
        return h.mapError(c, err, rid)
    }
    return c.Status(201).JSON(response.Success(result, rid))
}
```

---

## Step 6: Register Route

File: `routes/open_api/v1/<domain>.go`

```go
func Register<Domain>Routes(router fiber.Router, h *handler_<domain>.Handler) {
    router.Post("/<resource>", h.Create<Action>)
    router.Get("/<resource>/:id", h.Get<Action>)
}
```

Wire in `main.go`:
```go
openV1 := app.Group("/open/v1", middleware.AuthRequired(cfg))
routes.Register<Domain>Routes(openV1, handler_<domain>.NewHandler(facade))
```

---

## Step 7: Tests

Run the `test-engineer` agent to write:
1. Handler unit test (`handler_test.go`) — all response codes
2. Facade unit test (`facade_test.go`) — business logic paths

---

## Step 8: Verify

```bash
go build ./...
go vet ./...
go test -race ./apps/handlers/handler_<domain>/...
go test -race ./apps/facades/facade_<domain>/...
```

All must pass before marking the task complete.

---

## Common Mistakes

| Mistake | Correct Approach |
|---|---|
| Returning `gorm.ErrRecordNotFound` from handler | Map to `ErrNotFound` in repo, map to 404 in handler |
| Putting business logic in handler | Move to facade |
| Returning raw `models.Job` from facade | Map to DTO first |
| Importing `repository_<domain>` from handler | Handler imports facade interface only |
| `c.JSON(err.Error())` in error handler | Use `response.Error()` helper with `requestId` |
