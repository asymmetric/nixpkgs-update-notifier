// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.26.0
// source: query.sql

package db

import (
	"context"
	"database/sql"
)

const countPackagesByAttrPath = `-- name: CountPackagesByAttrPath :one
SELECT COUNT(*) FROM packages WHERE attr_path = ?
`

func (q *Queries) CountPackagesByAttrPath(ctx context.Context, attrPath string) (int64, error) {
	row := q.db.QueryRowContext(ctx, countPackagesByAttrPath, attrPath)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const countSubscriptionsByRoomIDAndAttrPath = `-- name: CountSubscriptionsByRoomIDAndAttrPath :one
SELECT COUNT(*) FROM subscriptions WHERE roomid = ? AND attr_path = ?
`

type CountSubscriptionsByRoomIDAndAttrPathParams struct {
	Roomid   string
	AttrPath string
}

func (q *Queries) CountSubscriptionsByRoomIDAndAttrPath(ctx context.Context, arg CountSubscriptionsByRoomIDAndAttrPathParams) (int64, error) {
	row := q.db.QueryRowContext(ctx, countSubscriptionsByRoomIDAndAttrPath, arg.Roomid, arg.AttrPath)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const createPackage = `-- name: CreatePackage :exec
INSERT OR IGNORE INTO packages(attr_path) VALUES (?)
`

func (q *Queries) CreatePackage(ctx context.Context, attrPath string) error {
	_, err := q.db.ExecContext(ctx, createPackage, attrPath)
	return err
}

const createSubscription = `-- name: CreateSubscription :exec
INSERT INTO subscriptions(roomid,attr_path,mxid) VALUES (?, ?, ?)
`

type CreateSubscriptionParams struct {
	Roomid   string
	AttrPath string
	Mxid     string
}

func (q *Queries) CreateSubscription(ctx context.Context, arg CreateSubscriptionParams) error {
	_, err := q.db.ExecContext(ctx, createSubscription, arg.Roomid, arg.AttrPath, arg.Mxid)
	return err
}

const deleteSubscription = `-- name: DeleteSubscription :execresult
DELETE FROM subscriptions WHERE roomid = ? AND attr_path = ?
`

type DeleteSubscriptionParams struct {
	Roomid   string
	AttrPath string
}

func (q *Queries) DeleteSubscription(ctx context.Context, arg DeleteSubscriptionParams) (sql.Result, error) {
	return q.db.ExecContext(ctx, deleteSubscription, arg.Roomid, arg.AttrPath)
}

const deleteSubscriptionsByRoomID = `-- name: DeleteSubscriptionsByRoomID :exec
DELETE FROM subscriptions WHERE roomid = ?
`

func (q *Queries) DeleteSubscriptionsByRoomID(ctx context.Context, roomid string) error {
	_, err := q.db.ExecContext(ctx, deleteSubscriptionsByRoomID, roomid)
	return err
}

const getAttrPaths = `-- name: GetAttrPaths :many
SELECT attr_path FROM subscriptions
`

func (q *Queries) GetAttrPaths(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, getAttrPaths)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var attr_path string
		if err := rows.Scan(&attr_path); err != nil {
			return nil, err
		}
		items = append(items, attr_path)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getAttrPathsByRoomID = `-- name: GetAttrPathsByRoomID :many
SELECT attr_path FROM subscriptions WHERE roomid = ?
`

func (q *Queries) GetAttrPathsByRoomID(ctx context.Context, roomid string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, getAttrPathsByRoomID, roomid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var attr_path string
		if err := rows.Scan(&attr_path); err != nil {
			return nil, err
		}
		items = append(items, attr_path)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getLastVisitedByAttrPath = `-- name: GetLastVisitedByAttrPath :one
SELECT last_visited FROM packages WHERE attr_path = ?
`

func (q *Queries) GetLastVisitedByAttrPath(ctx context.Context, attrPath string) (sql.NullString, error) {
	row := q.db.QueryRowContext(ctx, getLastVisitedByAttrPath, attrPath)
	var last_visited sql.NullString
	err := row.Scan(&last_visited)
	return last_visited, err
}

const getRoomIDsByAttrPath = `-- name: GetRoomIDsByAttrPath :many
SELECT roomid FROM subscriptions WHERE attr_path = ?
`

func (q *Queries) GetRoomIDsByAttrPath(ctx context.Context, attrPath string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, getRoomIDsByAttrPath, attrPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var roomid string
		if err := rows.Scan(&roomid); err != nil {
			return nil, err
		}
		items = append(items, roomid)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updatePackageLastVisited = `-- name: UpdatePackageLastVisited :exec
UPDATE packages SET last_visited = ?, error = ? WHERE attr_path = ?
`

type UpdatePackageLastVisitedParams struct {
	LastVisited sql.NullString
	Error       bool
	AttrPath    string
}

func (q *Queries) UpdatePackageLastVisited(ctx context.Context, arg UpdatePackageLastVisitedParams) error {
	_, err := q.db.ExecContext(ctx, updatePackageLastVisited, arg.LastVisited, arg.Error, arg.AttrPath)
	return err
}