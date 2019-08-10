package main

import (
	"database/sql"
	"log"
	"math/rand"
	"time"
)

var CODELEN = 7
var UUIDKEYLEN = 200
var (
	DEFAULTMIN = 10
	MAXMINS    = 60
)
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
	PublicKey   string    `json:"-"`
	UUID        string    `json:"-"`
	Code        string    `json:"user_code"`
	Bandwidth   int       `json:"bw_left"`
	MaxFileSize int       `json:"max_fs"`
	EndTime     time.Time `json:"end_time"`
	MinsAllowed int       `json:"mins_allowed"`
	WantedMins  int       `json:"wanted_mins"`
	Tier        int       `json:"user_tier"`
	Credit      float64   `json:"credit"`
	UUIDKey     string    `json:"UUID_key"`
}

func CreateNewUser(db *sql.DB, user User) {
	_, err := db.Exec(`
	INSERT INTO user (code, UUID, UUID_key, public_key, code_end_dttm, registered_dttm)
	VALUES (?, ?, ?, ?, ?, NOW())`, user.Code, Hash(user.UUID), Hash(user.UUIDKey), user.PublicKey, user.EndTime)
	Handle(err)
}

func UpdateUser(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`
	UPDATE user 
	SET code = ?, public_key = ?, wanted_mins = ?, code_end_dttm = ?
	WHERE UUID=?`, user.Code, user.PublicKey, user.WantedMins, user.EndTime, Hash(user.UUID)))
}

func SetUsersTier(db *sql.DB, user *User) {
	SetUsersCredit(db, user)
	user.Tier = FREEUSER
	if user.Credit >= CODECRED {
		user.Tier = CODEUSER
	} else if user.Credit >= PERMCRED {
		user.Tier = PERMUSER
	} else if user.Credit > 0 {
		user.Tier = PAIDUSER
	}
}

func SetUsersMinsAllowed(user *User) {
	if user.Tier == CODEUSER {
		user.MinsAllowed = 60
	} else if user.Tier == PERMUSER {
		user.MinsAllowed = 30
	} else if user.Tier == PAIDUSER {
		user.MinsAllowed = 20
	}
	user.MinsAllowed = DEFAULTMIN
}

func DeleteCode(db *sql.DB, user User) error {
	return UpdateErr(db.Exec(`UPDATE user
	SET code = NULL, code_end_dttm = NULL
	WHERE UUID = ? AND UUID_key = ?`, user.UUID, user.UUIDKey))
}

func SetUserStats(db *sql.DB, user *User) {
	// get user time left
	SetUserCodeEndTime(db, user)
	if user.EndTime.Sub(time.Now()) <= 0 {
		// user has expired
		go DeleteCode(db, *user)
	}

	SetUsersMinsAllowed(user)
	SetUsersTier(db, user)
	user.Bandwidth = GetUserBandwidthLeft(db, user)
	SetUsersMaxFileUpload(db, user)
}

func UserSocketConnected(db *sql.DB, user User, connected bool) error {
	isConnected := 1
	if connected {
		log.Println("Connected:", Hash(user.UUID))
	} else {
		isConnected = 0
		log.Println("Disconnected:", Hash(user.UUID))
	}
	return UpdateErr(db.Exec(`UPDATE user
	SET is_connected = ?
	WHERE UUID = ? AND UUID_key = ?`, isConnected, user.UUID, user.UUIDKey))
}

func HasUUID(db *sql.DB, user User) bool {
	var id int
	result := db.QueryRow(`
	SELECT id
		FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&id)
	if err == nil && id > 0 {
		return true
	}
	return false
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
	SetUsersCredit(db, user)
	usedBandwidth := GetUserUsedBandwidth(db, *user)
	creditedBandwidth := CreditToBandwidth(user.Credit)
	return creditedBandwidth - usedBandwidth
}

func SetUsersMaxFileUpload(db *sql.DB, user *User) {
	SetUsersCredit(db, user)
	user.MaxFileSize = CreditToFileUpload(user.Credit)
}

func SetUserCodeEndTime(db *sql.DB, user *User) {
	result := db.QueryRow(`SELECT code_end_dttm
	FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&user.EndTime)
	if err != nil || user.EndTime.IsZero() {
		user.EndTime = time.Now().UTC()
	}
}

func CodeToUser(db *sql.DB, code string) (user User) {
	if len(code) != CODELEN {
		return
	}
	result := db.QueryRow(`SELECT UUID, public_key
	FROM user
	WHERE code = ?
	AND code_end_dttm >= NOW()`, code)
	err := result.Scan(&user.UUID, &user.PublicKey)
	Handle(err)
	return
}

func IsValidUserCredentials(db *sql.DB, user User) bool {
	if IsValidUUID(user.UUID) {
		var id int64
		result := db.QueryRow(`SELECT id
        FROM user
        WHERE UUID = ? AND UUID_key = ?`, Hash(user.UUID), Hash(user.UUIDKey))
		err := result.Scan(&id)
		Handle(err)
		if err == nil && id > 0 {
			return true
		}
	}
	return false
}

func codeExists(db *sql.DB, code string) bool {
	var id int
	result := db.QueryRow(`SELECT id
	FROM user
	WHERE code = ?`, code)
	err := result.Scan(&id)
	return err == nil && id > 0
}

func GenUserCode(db *sql.DB) string {
	var letter = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	for {
		b := make([]rune, CODELEN)
		for i := range b {
			b[i] = letter[rand.Intn(len(letter))]
		}
		code := string(b)
		if !codeExists(db, code) {
			return code
		}
	}
}

func SetUserWantedMins(db *sql.DB, user *User) {
	result := db.QueryRow(`SELECT wanted_mins
	FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&user.WantedMins)
	Handle(err)
}
