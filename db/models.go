// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.26.0

package db

import (
	"database/sql"
)

type Package struct {
	AttrPath    string
	LastVisited sql.NullString
	Error       bool
}

type Subscription struct {
	ID       int64
	Roomid   string
	Mxid     string
	AttrPath string
}