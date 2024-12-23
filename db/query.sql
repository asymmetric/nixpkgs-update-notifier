-- name: CountPackagesByAttrPath :one
SELECT COUNT(*) FROM packages WHERE attr_path = ?;

-- name: CreatePackage :exec
INSERT OR IGNORE INTO packages(attr_path) VALUES (?);

-- name: GetAttrPaths :many
SELECT attr_path FROM subscriptions;

-- name: GetLastVisitedByAttrPath :one
SELECT last_visited FROM packages WHERE attr_path = ?;

-- name: UpdatePackageLastVisited :exec
UPDATE packages SET last_visited = ?, error = ? WHERE attr_path = ?;

-- name: DeleteSubscriptionsByRoomID :exec
DELETE FROM subscriptions WHERE roomid = ?;

-- name: DeleteSubscription :execresult
DELETE FROM subscriptions WHERE roomid = ? AND attr_path = ?;

-- name: GetAttrPathsByRoomID :many
SELECT attr_path FROM subscriptions WHERE roomid = ?;

-- name: CountSubscriptionsByRoomIDAndAttrPath :one
SELECT COUNT(*) FROM subscriptions WHERE roomid = ? AND attr_path = ?;

-- name: CreateSubscription :exec
INSERT INTO subscriptions(roomid,attr_path,mxid) VALUES (?, ?, ?);

-- name: GetRoomIDsByAttrPath :many
SELECT roomid FROM subscriptions WHERE attr_path = ?;
