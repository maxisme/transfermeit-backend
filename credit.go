package main

import (
	"database/sql"
)

var CREDITCODELEN = 100

// set the users perm code as long as it is what they are expecting
func SetUsersPermCode(db *sql.DB, user *User, expectedPermCode string) {
	permCode, customCode := GetUsersPermCode(db, *user)
	var code string
	if customCode.Valid {
		code = customCode.String
	} else if permCode.Valid {
		code = permCode.String
	}
	if code == expectedPermCode {
		user.Code = expectedPermCode
	}
}

func GetUsersPermCode(db *sql.DB, user User) (permCode sql.NullString, customCode sql.NullString) {
	result := db.QueryRow(`
	SELECT perm_user_code, custom_user_code
	FROM credit
	WHERE UUID = ?
	LIMIT 1`, Hash(user.UUID))
	_ = result.Scan(&permCode, &customCode)
	return
}

func RemovePermCodes(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET perm_user_code = NULL, custom_user_code = NULL
	WHERE UUID=?`, Hash(user.UUID)))
}

func SetCustomCode(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET custom_user_code=?
	WHERE UUID=?`, user.Code, Hash(user.UUID)))
}

func SetPermCode(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET perm_user_code=?
	WHERE UUID=?`, user.Code, Hash(user.UUID)))
}

func SetCreditCode(db *sql.DB, user User, activationCode string) error {
	return UpdateErr(db.Exec(`
	UPDATE credit
	SET UUID=?, activation_dttm=NOW()
	WHERE activation_code=?
	AND UUID IS NULL`, Hash(user.UUID), activationCode))
}

func SetUsersCredit(db *sql.DB, user *User) {
	if user.Credit > 0 {
		// already set the users credit
		return
	}
	result := db.QueryRow(`SELECT SUM(credit) as total_credit
	FROM credit
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&user.Credit)
	if err != nil {
		user.Credit = 0
	}
}
