package main

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
)

type server struct {
	db *sql.DB
}

func DBConn(dataSourceName string) (db *sql.DB, err error) {
	db, err = sql.Open("mysql", dataSourceName)
	if err != nil {
		return
	}
	err = db.Ping()
	return
}
