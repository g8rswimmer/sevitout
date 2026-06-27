-- name: InsertUser :exec
INSERT INTO users (id, email, name, avatar_url, org_role, active, oauth_provider, oauth_subject, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetUser :one
SELECT id, email, name, avatar_url, org_role, active, oauth_provider, oauth_subject, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, name, avatar_url, org_role, active, oauth_provider, oauth_subject, created_at, updated_at
FROM users
WHERE email = $1;

-- name: UpdateUser :exec
UPDATE users SET
    name       = $2,
    avatar_url = $3,
    org_role   = $4,
    active     = $5,
    updated_at = $6
WHERE id = $1;

-- name: ListUsers :many
SELECT id, email, name, avatar_url, org_role, active, oauth_provider, oauth_subject, created_at, updated_at
FROM users
ORDER BY email;
