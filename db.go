package main

import (
	"database/sql"
	"github.com/dgraph-io/badger"
	_ "github.com/go-sql-driver/mysql"
)

type Server struct {
	db  *sql.DB
	bdb *badger.DB
}

func DBConn(dataSourceName string) (db *sql.DB, err error) {
	db, err = sql.Open("mysql", dataSourceName)
	if err != nil {
		return
	}
	err = db.Ping()
	return
}
