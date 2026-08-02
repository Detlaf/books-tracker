# Authentication Design — Backend Milestone 2

Date: 2026-08-02
Status: Approved, not yet implemented
Covers: `specs/backend/implementation.md` Milestone 2

## Goal

Let a user register, log in, and stay logged in on a mobile client for weeks, while giving
the server the ability to revoke a session. Every endpoint in Milestones 4–7 is scoped to
an authenticated user, so this milestone also establishes the protected-route pattern
those endpoints will use.

## Decisions

| Decision | Choice | Reason |
|---|---|---|
| Session model | Short access JWT + long opaque refresh token | Android needs long sessions; a single 30-day JWT cannot be revoked |
| Registration | Open, email + password, no verification | Personal-scale app; SMTP infrastructure is not worth it yet |
| Signing key | `JWT_SECRET` env var, required, ≥ 32 bytes | A default or auto-generated key is how test secrets reach production |
| Password hashing | bcrypt, cost 12 | Standard; `golang.org/x/crypto` is already a dependency |
| Refresh token storage | SHA-256 of a 256-bit random token | High-entropy tokens need no slow hash; a DB dump yields nothing usable |
| Code structure | Pure core (`internal/auth`) + thin gin handlers | Crypto logic testable without HTTP or a database |
| Tests | Written first | Auth bugs fail silently as security holes rather than visible breakage |

## Architecture

```
internal/auth/
  password.go   Hash(pw) / Verify(hash, pw)          — bcrypt only
  token.go      Signer: SignAccess / ParseAccess     — golang-jwt/jwt/v5 only
  service.go    Register / Login / Refresh / Logout  — depends on Store
  store.go      Store interface
  errors.go     ErrInvalidCredentials, ErrEmailTaken, ErrInvalidToken

internal/store/
  sqlite.go     Store implementation over *sql.DB

internal/server/
  auth_handlers.go  gin -> service -> JSON
  middleware.go     RequireAuth, userID(c) helper

internal/config/
  config.go     Load() -> Config{DSN, Addr, JWTSecret, AccessTTL, RefreshTTL}
```

`password.go` and `token.go` import neither gin nor `database/sql`. `service.go` depends on
the `Store` interface, so its tests use a fake. Only `internal/store` tests touch SQLite.

### Store interface

```go
type Store interface {
    CreateUser(ctx context.Context, email, passwordHash string) (User, error)
    UserByEmail(ctx context.Context, email string) (User, error)
    CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
    RefreshTokenByHash(ctx context.Context, tokenHash string) (RefreshToken, error)
    RevokeRefreshToken(ctx context.Context, tokenHash string) error
    RevokeAllForUser(ctx context.Context, userID int64) error
}
```

`RefreshTokenByHash` returns the row **regardless of its `revoked_at` or `expires_at` state**,
and the service inspects those fields. Filtering revoked rows out in SQL would collapse
"unknown token" and "revoked token" into the same not-found result, which would make reuse
detection impossible. Only a hash with no row at all is a not-found.

## Tokens

**Access token** — JWT, HS256, 15-minute TTL, claims `{sub: userID, exp, iat}`. Stateless:
`RequireAuth` verifies the signature without a database round trip.

`ParseAccess` passes `jwt.WithValidMethods([]string{"HS256"})`, rejecting a token re-signed
with `alg: none` or an asymmetric algorithm. This is the standard JWT vulnerability and the
guard is one argument.

**Refresh token** — not a JWT. 32 bytes from `crypto/rand`, base64url-encoded, opaque to the
client, 30-day TTL, stored server-side as a SHA-256 hash so it can be revoked.

### Rotation and reuse detection

Every `/auth/refresh` revokes the presented token and issues a new pair. If a token that is
already revoked is presented, a stolen token is being replayed: revoke every token for that
user and return 401, forcing a fresh login. This bounds token theft to a single-use window.

## Schema — migration 000003

```sql
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT     NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
```

Down migration drops the index and the table.

Expired rows are left in place; there is no cleanup job in this milestone. Volume is one row
per login per device, and expiry is enforced in the query rather than by deletion.

## API

| Endpoint | Request | Success | Body |
|---|---|---|---|
| `POST /auth/register` | `{email, password}` | 201 | `{"id": 1, "email": "a@b.c"}` |
| `POST /auth/login` | `{email, password}` | 200 | token pair (below) |
| `POST /auth/refresh` | `{refresh_token}` | 200 | token pair |
| `POST /auth/logout` | `{refresh_token}` | 204 | empty |

Token pair:

```json
{
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": 900,
  "refresh_token": "b64url..."
}
```

`logout` revokes only the presented token, so signing out on a phone does not sign out a
browser. A log-out-everywhere endpoint is out of scope; `RevokeAllForUser` already exists for
reuse detection if it is wanted later.

`logout` is idempotent: an unknown, already-revoked, or expired refresh token still returns
204. A client that has lost track of its session state should be able to log out cleanly, and
the endpoint reveals nothing either way. Only a malformed request body returns 400.

Emails are normalized — trimmed and lowercased — before storage and before every lookup, so
`A@B.com` and `a@b.com` are one account. Without this the `UNIQUE` constraint on `users.email`
would permit case-variant duplicates and login would depend on typing capitalization the same
way twice.

### Protected route pattern

```go
authed := s.router.Group("/", RequireAuth(s.signer))
authed.GET("/library", s.listLibrary)   // Milestone 4 onward
```

`RequireAuth` reads `Authorization: Bearer <jwt>`, verifies it, and stores the user ID with
`c.Set(userIDKey, id)`. Handlers read it through one typed helper, `userID(c) int64`, instead
of each repeating an unchecked type assertion.

## Errors

Envelope: `{"error": "message"}`.

| Status | Cases |
|---|---|
| 400 | malformed JSON; invalid email syntax; password < 8 bytes or > 72 bytes |
| 401 | bad credentials; missing, malformed, expired, or invalid token; replayed refresh token |
| 409 | email already registered |
| 500 | unexpected failure — logged server-side, generic message returned |

**Login does not reveal whether an email exists.** Both "no such user" and "wrong password"
return 401 `"invalid email or password"`. When the user is not found, a bcrypt comparison
against a fixed dummy hash still runs, so response time does not leak existence either.
Registration necessarily leaks it through 409, which is the accepted trade for a usable
error message.

**Password rules stop at length.** Minimum 8 bytes, no composition requirements. Input over
bcrypt's 72-byte limit is rejected with 400 rather than silently truncated.

## Configuration

`config.Load()` reads:

| Variable | Required | Default |
|---|---|---|
| `JWT_SECRET` | yes, ≥ 32 bytes | none — server refuses to boot |
| `DATABASE_URL` | no | `book_tracking.db` |
| `ADDR` | no | `:8080` |

A missing or short `JWT_SECRET` aborts startup with a message naming the fix
(`openssl rand -base64 32`). No fallback in any environment.

## Tests

Written before the implementation.

```
internal/auth/password_test.go  hash != plaintext; verify round-trip; wrong password;
                                >72-byte input rejected
internal/auth/token_test.go     sign/parse round-trip; expired; wrong secret;
                                alg:none rejected; tampered payload rejected
internal/auth/service_test.go   register/login/refresh/logout against a fake Store;
                                duplicate email; case-variant email is the same account;
                                rotation revokes the old token; expired refresh rejected;
                                replaying a revoked token revokes the family;
                                logout of an unknown token still succeeds
internal/server/auth_test.go    full HTTP path: every status code above;
                                Bearer parsing; missing/malformed header -> 401
```

`newTestServer(t)` builds an in-memory SQLite database, runs migrations, and returns a wired
server. Milestones 4–7 reuse this helper, so it is built to be reused.

## Dependencies

- `github.com/golang-jwt/jwt/v5` — new
- `golang.org/x/crypto/bcrypt` — already present as an indirect dependency

## Out of scope

Email verification, password reset, OAuth or social login, rate limiting on login,
log-out-everywhere, expired-token cleanup, and refresh-token binding to a device or IP.
