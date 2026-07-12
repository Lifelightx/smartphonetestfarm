// Package coordinator_server implements coordinator HTTP, WebSockets, and administrative APIs.
//
// File: db.go
// This file contains implementation and helper structures for coordinator HTTP, WebSockets, and administrative APIs.

package coordinator_server

import (
	"protean-provider/internal/db"
)

type DB = db.DB
type ScriptDB = db.ScriptDB
type ReportDB = db.ReportDB

var OpenDB = db.OpenDB
