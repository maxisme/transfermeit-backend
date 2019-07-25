package main

import "database/sql"

func getPermCode(db *sql.DB, user User) (code string) {
	result := db.QueryRow(`
	SELECT perm_user_code 
	FROM pro
	WHERE UUID = ?
	LIMIT 1`, Hash(user.UUID))
	_ = result.Scan(code)
	return
}
