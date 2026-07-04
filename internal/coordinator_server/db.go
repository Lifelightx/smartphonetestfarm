package coordinator_server

import (
	"protean-provider/internal/db"
)

type DB = db.DB
type ScriptDB = db.ScriptDB
type ReportDB = db.ReportDB

var OpenDB = db.OpenDB
