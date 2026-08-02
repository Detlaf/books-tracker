# Authentication Design — Backend Milestone 2

Date: 2026-08-02
Status: Implemented on `feat/auth-milestone-2`, amended after branch review
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
    RevokeRefreshToken(ctx context.Context, tokenHash, reason string) (revoked bool, err error)
    RevokeAllForUser(ctx context.Context, userID int64) error
}
```

`RefreshTokenByHash` returns the row **regardless of its `revoked_at` or `expires_at` state**,
and the service inspects those fields. Filtering revoked rows out in SQL would collapse
"unknown token" and "revoked token" into the same not-found result, which would make reuse
detection impossible. Only a hash with no row at all is a not-found.

`RevokeRefreshToken` must be **atomic**: it revokes only a row that is still live, and reports
whether this call is the one that revoked it. In SQLite that is
`UPDATE ... WHERE token_hash = ? AND revoked_at IS NULL` returning `RowsAffected() > 0`. A hash
that is unknown or already revoked is `(false, nil)`, not an error — rotation decides on that
bool, and logout relies on it to stay idempotent.

## Tokens

**Access token** — JWT, HS256, 15-minute TTL, claims `{sub: userID, exp, iat}`. Stateless:
`RequireAuth` verifies the signature without a database round trip.

`ParseAccess` passes `jwt.WithValidMethods([]string{"HS256"})`, rejecting a token re-signed
with `alg: none` or an asymmetric algorithm. This is the standard JWT vulnerability and the
guard is one argument.

**Refresh token** — not a JWT. 32 bytes from `crypto/rand`, base64url-encoded, opaque to the
client, 30-day TTL, stored server-side as a SHA-256 hash so it can be revoked.

### Rotation and reuse detection

Every `/auth/refresh` revokes the presented token and issues a new pair.

**The revoke is the decision, not the read.** Reading `revoked_at`, deciding, then writing is a
race: two requests presenting the same live token both read `revoked_at IS NULL`, both pass the
check, and both are issued a valid pair — which is precisely the thief-races-the-victim case
rotation exists to catch. Instead the service attempts the atomic revoke and decides on its
result. Only one caller can win it; whoever loses presented a token that was already spent, and
is handled as a replay. The `revoked_at != nil` check survives only as a fast path that avoids
an attempted write.

**What a replay costs depends on why the token was revoked.** Treating every replay as theft
makes a permanent-lockout primitive: a client retrying a refresh after a dropped response, or
anyone replaying one stale logged-out token in a loop, would revoke every session the user has.
So `refresh_tokens.revoked_reason` records why:

| `revoked_reason` | Set by | Replaying it |
|---|---|---|
| `rotated` | `/auth/refresh` | Theft. `RevokeAllForUser`, then 401 |
| `logout` | `/auth/logout` | 401 only — no cascade |
| `""` (NULL) | rows written before migration 000004 | 401 only — no cascade |

Empty is deliberately non-cascading: pre-migration rows carry no reason, and reading absence of
evidence as evidence of theft would log those users out everywhere.

This bounds token theft to a single-use window in the common case: a stolen token is either used
before the victim's next refresh, in which case the victim's next attempt loses the race and burns
the family, or used after, in which case it is already `rotated` and burns the family immediately.
One interleaving escapes that bound — see "a race winner's new token can outlive the cascade" below.

### Known limitations

- **A retried rotation trips family revocation.** If a client's `/auth/refresh` response is lost
  and it retries with the same token, that is indistinguishable from theft and revokes the
  family. Only logged-out tokens are exempt. The alternative — a grace window serving the same
  new pair twice — was not worth the state it needs at this scale.
- **Login and registration are a CPU-amplification vector.** Both are unauthenticated and both
  spend a full bcrypt cost-12 comparison (login does so even for an unknown email, by design, to
  avoid a timing oracle). Rate limiting remains out of scope; this is recorded and accepted.
- **Expired `refresh_tokens` rows are never cleaned up.** Acceptable at this scale — one row per
  login per device, and lookups are index-bound on `token_hash`. Revisit if a sessions UI is
  added, which would make the dead rows user-visible.
- **A race winner's new token can outlive the cascade.** `Refresh` revokes and then issues in two
  separate statements. If the loser's `RevokeAllForUser` runs between the winner's `UPDATE` and its
  `CreateRefreshToken`, the winner's fresh token is inserted after the sweep and stays live. Where
  the winner is a thief, that leaves the attacker with a session while the victim is locked out —
  the inverse of the intended outcome. The window is the few hundred microseconds between the two
  statements and requires an attacker already holding a stolen token and racing deliberately.
  Closing it means running the revoke and the insert in one transaction, which is the natural
  follow-up; it was left out here because it widens the `Store` contract to expose transactions.

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

## Schema — migration 000004

```sql
ALTER TABLE refresh_tokens ADD COLUMN revoked_reason TEXT;
```

Nullable, so existing rows need no backfill: NULL reads as `""`, which is non-cascading. Down
migration drops the column.

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
(`openssl rand -base64 32`). No fallback in any environment. The value is trimmed of
surrounding whitespace before the length check, so the trailing newline left by
`docker secret` or `kubectl create secret --from-file` does not become part of the signing key
in one environment and not another.

`DATABASE_URL` is joined with the connection parameters `_foreign_keys=on`,
`_busy_timeout=5000`, and `_journal_mode=WAL`, respecting a query string it may already carry.
The busy timeout and WAL matter because this is the first milestone whose requests write
concurrently.

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
                                replaying a rotated token revokes the family;
                                replaying a logged-out token does not;
                                losing the revoke race is treated as a replay;
                                a store error is not flattened into bad credentials;
                                logout of an unknown token still succeeds
internal/server/auth_test.go    full HTTP path: every status code above;
                                rotation over the real stack: a pre-rotation token -> 401;
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
