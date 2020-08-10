package main

import (
	"database/sql"
)

// CreditCodeLen is the length of the randomly generated code to be used to activate credit on a users account
const CreditCodeLen = 100

// GetUserPermCode requests a users perm code if they have one
func GetUserPermCode(db *sql.DB, user User) (permCode sql.NullString, customCode sql.NullString, err error) {
	result := db.QueryRow(`
	SELECT perm_user_code, custom_user_code
	FROM credit
	WHERE UUID = ?
	LIMIT 1`, Hash(user.UUID))
	err = result.Scan(&permCode, &customCode)
	return
}

// RemovePermCodes will remove the users stored perm code
func RemovePermCodes(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET perm_user_code = NULL, custom_user_code = NULL
	WHERE UUID=?`, Hash(user.UUID)))
}

// SetCustomCode sets a permanent custom code for a user
func SetCustomCode(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET custom_user_code=?
	WHERE UUID=?`, user.Code, Hash(user.UUID)))
}

// SetPermCode sets a permanent code for a user
func SetPermCode(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET perm_user_code=?
	WHERE UUID=?`, user.Code, Hash(user.UUID)))
}

// SetCreditCode associates a credit code to an account
func SetCreditCode(db *sql.DB, user User, activationCode string) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET UUID=?, activation_dttm=NOW()
	WHERE activation_code=?
	AND UUID IS NULL`, Hash(user.UUID), activationCode))
}

// GetCredit fetches the amount of credit the user has linked to their account
func GetCredit(db *sql.DB, user User) (credit sql.NullFloat64, err error) {
	result := db.QueryRow(`SELECT SUM(credit) as total_credit
	FROM credit
	WHERE UUID = ?`, Hash(user.UUID))
	err = result.Scan(&credit)
	return credit, nil
}
