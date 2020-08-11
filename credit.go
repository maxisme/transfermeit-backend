package main

import (
	"database/sql"
	tdb "github.com/maxisme/transfermeit-backend/tracer/db"
	"net/http"
)

// CreditCodeLen is the length of the randomly generated code to be used to activate credit on a users account
const CreditCodeLen = 100

// GetUserPermCode requests a users perm code if they have one
func GetUserPermCode(r *http.Request, db *sql.DB, user User) (permCode sql.NullString, customCode sql.NullString, err error) {
	result := tdb.QueryRow(r, db, `
	SELECT perm_user_code, custom_user_code
	FROM credit
	WHERE UUID = ?
	LIMIT 1`, Hash(user.UUID))
	err = result.Scan(&permCode, &customCode)
	return
}

// RemovePermCodes will remove the users stored perm code
func RemovePermCodes(r *http.Request, db *sql.DB, user User) error {
	return UpdateErr(tdb.Exec(r, db, `
	UPDATE credit
	SET perm_user_code = NULL, custom_user_code = NULL
	WHERE UUID=?`, Hash(user.UUID)))
}

// SetCustomCode sets a permanent custom code for a user
func SetCustomCode(r *http.Request, db *sql.DB, user User) error {
	return UpdateErr(tdb.Exec(r, db, `
	UPDATE credit
	SET custom_user_code=?
	WHERE UUID=?`, user.Code, Hash(user.UUID)))
}

// SetPermCode sets a permanent code for a user
func SetPermCode(r *http.Request, db *sql.DB, user User) error {
	return UpdateErr(tdb.Exec(r, db, `
	UPDATE credit
	SET perm_user_code=?
	WHERE UUID=?`, user.Code, Hash(user.UUID)))
}

// SetCreditCode associates a credit code to an account
func SetCreditCode(r *http.Request, db *sql.DB, user User, activationCode string) error {
	return UpdateErr(tdb.Exec(r, db, `
	UPDATE credit
	SET UUID=?, activation_dttm=NOW()
	WHERE activation_code=?
	AND UUID IS NULL`, Hash(user.UUID), activationCode))
}

// GetCredit fetches the amount of credit the user has linked to their account
func GetCredit(r *http.Request, db *sql.DB, user User) (credit sql.NullFloat64, err error) {
	result := tdb.QueryRow(r, db, `SELECT SUM(credit) as total_credit
	FROM credit
	WHERE UUID = ?`, Hash(user.UUID))
	err = result.Scan(&credit)
	return credit, nil
}
