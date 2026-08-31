-- name: InsertUser :exec
INSERT INTO users (id, email, name, avatar_url, org_role, active, password_hash, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetUser :one
SELECT id, email, name, avatar_url, org_role, active, password_hash,
       slack_user_id, github_username, jira_account_id, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, name, avatar_url, org_role, active, password_hash,
       slack_user_id, github_username, jira_account_id, created_at, updated_at
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

-- name: UpdateUserPassword :exec
UPDATE users SET
    password_hash = $2,
    updated_at    = $3
WHERE id = $1;

-- name: UpdateUserIntegrationIdentities :exec
UPDATE users SET
    slack_user_id    = $2,
    github_username  = $3,
    jira_account_id  = $4,
    updated_at       = $5
WHERE id = $1;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: ListUsers :many
SELECT id, email, name, avatar_url, org_role, active, password_hash,
       slack_user_id, github_username, jira_account_id, created_at, updated_at
FROM users
ORDER BY email;
