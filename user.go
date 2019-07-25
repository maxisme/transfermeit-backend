package main

import (
	"database/sql"
	"log"
	"time"
)

var CODELEN = 7
var UUIDKEYLEN = 200
var DEFAULTMIN = 10
var (
	FREEUSER = 0
	PAIDUSER = 1
	PERMUSER = 2
	CODEUSER = 3
)

//£ user has of credit
var (
	PERMCRED = 5.0
	CODECRED = 10.0
)

type User struct {
	ID          int       `json:"-"`
	Code        string    `json:"user_code"`
	Bandwidth   int       `json:"bw_left"`
	MaxFileSize int       `json:"max_fs"`
	TimeLeft    time.Time `json:"time_left"`
	MinsAllowed int       `json:"mins_allowed"`
	Tier        int       `json:"user_tier"`
	Credit      float64   `json:"credit"`
	UUID        string
	UUIDKey     string
	PublicKey   string
}

func CreateNewUser(db *sql.DB, user User) bool {
	_, err := db.Exec(`
	INSERT INTO user (code, UUID, UUID_key, public_key, created_dttm, registered_dttm)
	VALUES (?, ?, ?, ?, NOW(), NOW())`, user.Code, Hash(user.UUID), Hash(user.UUIDKey), user.PublicKey)
	Err(err)
	if err != nil {
		return false
	}
	return true
}

func UpdateUser(db *sql.DB, user User, wantedMins int) {
	_, err := db.Exec(`
	UPDATE user 
	SET code = ?, public_key = ?, wanted_mins = ?, created_dttm = NOW()
	WHERE UUID=?`, user.Code, user.PublicKey, wantedMins, Hash(user.UUID))
	Err(err)
}

func SetUserTier(db *sql.DB, user *User) {
	SetUserCredit(db, user)
	user.Tier = FREEUSER
	if user.Credit >= CODECRED {
		user.Tier = CODEUSER
	} else if user.Credit >= PERMCRED {
		user.Tier = PERMUSER
	} else if user.Credit > 0 {
		user.Tier = PAIDUSER
	}
}

func SetUserMinsAllowed(user *User) {
	if user.Tier == CODEUSER {
		user.MinsAllowed = 60
	} else if user.Tier == PERMUSER {
		user.MinsAllowed = 30
	} else if user.Tier == PAIDUSER {
		user.MinsAllowed = 20
	}
	user.MinsAllowed = DEFAULTMIN
}

func DeleteCode(db *sql.DB, user User) {
	_, err := db.Exec(`UPDATE user
	SET code = NULL
	WHERE UUID = ? AND UUID_key = ?`, user.UUID, user.UUIDKey)
	Err(err)
}

func SetUserStats(db *sql.DB, user *User) {
	// get user time left
	timeLeft := GetUserCodeTimeLeft(db, *user)
	if timeLeft.Sub(time.Now()) > 0 {
		user.TimeLeft = timeLeft
	} else {
		// user has expired
		go DeleteCode(db, *user)
	}

	// get user tier
	SetUserTier(db, user)

	// get user bandwidth left
	user.Bandwidth = GetUserBandwidthLeft(db, user)

	// get user max upload file size
	SetMaxFileUpload(db, user)
}

func UserSocketConnected(db *sql.DB, user User, connected bool) {
	_, err := db.Exec(`UPDATE user
	SET is_connected = ?
	WHERE UUID = ? AND UUID_key = ?`, connected, user.UUID, user.UUIDKey)
	Err(err)

	if connected {
		log.Println("Connected:", Hash(user.UUID))
	} else {
		log.Println("Disconnected:", Hash(user.UUID))
	}
}

func HasUUID(db *sql.DB, user User) bool {
	var id int
	result := db.QueryRow(`
	SELECT id
		FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(id)
	if err == nil && id > 0 {
		return true
	}
	return false
}

func SetUserCredit(db *sql.DB, user *User) {
	if user.Credit > 0 {
		// already have the users credit
		return
	}
	result := db.QueryRow(`SELECT SUM(credit) as total_credit
	FROM pro
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&user.Credit)
	if err != nil {
		user.Credit = 0
	}
	return
}

func GetUserUsedBandwidth(db *sql.DB, user User) (bytes int) {
	extra := ``
	if user.Tier > FREEUSER {
		extra = `
		AND DATE(finished_ddtm) = CURDATE()
		AND pro = 0`
	}
	result := db.QueryRow(`SELECT SUM(size) as used_bandwidth
	FROM upload
	WHERE from_UUID = ? `+extra, Hash(user.UUID))
	err := result.Scan(&bytes)
	if err != nil {
		bytes = 0
	}
	return
}

func GetUserBandwidthLeft(db *sql.DB, user *User) int {
	SetUserCredit(db, user)
	usedBandwidth := GetUserUsedBandwidth(db, *user)
	creditedBandwidth := CreditToBandwidth(user.Credit)
	return creditedBandwidth - usedBandwidth
}

func SetMaxFileUpload(db *sql.DB, user *User) {
	SetUserCredit(db, user)
	user.MaxFileSize = CreditToFileUpload(user.Credit)
}

func GetUserCodeTimeLeft(db *sql.DB, user User) time.Time {
	var created time.Time
	var wantedMins int
	result := db.QueryRow(`SELECT created_dttm, wanted_mins
	FROM user
	WHERE UUID = ? AND UUID_key = ?`, Hash(user.UUID), Hash(user.UUIDKey))
	err := result.Scan(&created, &wantedMins)
	Err(err)

	return created.Add(time.Second * time.Duration(wantedMins))
}

func CodeToUser(db *sql.DB, code string) (user User) {
	if len(code) != CODELEN {
		return
	}
	result := db.QueryRow(`SELECT UUID, public_key
	FROM user
	WHERE code = ?`, code)
	err := result.Scan(&user.UUID, &user.PublicKey)
	Err(err)
	return
}

func IsValidUserCredentials(db *sql.DB, user User) bool {
	if IsValidUUID(user.UUID) {
		var id int64
		result := db.QueryRow(`SELECT id
        FROM user
        WHERE UUID = ? AND UUID_key = ?`, Hash(user.UUID), Hash(user.UUIDKey))
		err := result.Scan(&id)
		if err == nil && id > 0 {
			return true
		}
	}
	return false
}
