# Skill: API Contracts

## When to use

Use this skill when defining or modifying HTTP endpoints, SDK interfaces, request/response types, or OpenAPI specifications.

## Rules

1. API contracts are defined in `api/openapi/`.
2. HTTP handlers live in `internal/adapters/inbound/http/`.
3. SDK interfaces live in `internal/adapters/inbound/sdk/`.
4. Handlers must be thin — delegate to application services.
5. Request validation happens at the adapter boundary.
6. Domain errors are mapped to HTTP status codes at the adapter level.
7. No business logic in handlers.

## HTTP conventions

- Use standard REST verbs (POST for creation, GET for retrieval, etc.).
- Return structured error responses with error codes.
- Version the API path (e.g., `/api/v1/...`).

## Checklist

- [ ] Endpoint is documented in OpenAPI spec.
- [ ] Handler delegates to application service.
- [ ] Request is validated before reaching application layer.
- [ ] Domain errors are translated to HTTP responses.
- [ ] No business logic in the handler.

## References

- `docs/architecture.md`
- `docs/rules.md` §3
