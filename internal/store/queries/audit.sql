-- Queries used by internal/store/audit.go. The hand-written wrapper exists
-- because sealing the hash chain and inserting must happen in one transaction.

-- name: LastAuditEvent :one
SELECT * FROM audit_events
WHERE org_id = ?1
ORDER BY seq DESC
LIMIT 1;

-- name: InsertAuditEvent :exec
INSERT INTO audit_events (
    id, org_id, seq, prev_hash, hash, actor_id, actor_kind, actor_name,
    action, resource_type, resource_id, resource_name, project_id, env_id,
    details, ip, user_agent, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?);

-- name: ListAuditEvents :many
SELECT * FROM audit_events
WHERE org_id = ?1 AND seq > ?2
ORDER BY seq ASC
LIMIT ?3;
