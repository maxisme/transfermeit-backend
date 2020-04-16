package tmi

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
)

// Server is used for database pooling - sharing the db connection to the web handlers.
type Server struct {
	DB *sql.DB
}

func DbConn(dataSourceName string) (db *sql.DB, err error) {
	db, err = sql.Open("mysql", dataSourceName)
	if err != nil {
		return
	}
	err = db.Ping()
	return
}
