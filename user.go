package main

import (
	"database/sql"
	"github.com/patrickmn/go-cache"
	"log"
	"math/rand"
	"time"
)

const CodeLen = 7
const UUIDKeyLen = 200
const (
	DefaultAccountLifeMins = 10
	MaxAccountLifeMins     = 60
)
const (
	FreeUserTier       = 0
	PaidUserTier       = 1
	PermUserTier       = 2
	CustomCodeUserTier = 3
)

const (
	PermCodeCreditAmt   = 5.0  // once user has this much credit they will have PermUserTier
	CustomCodeCreditAmt = 10.0 // once user has this much credit they will have CustomCodeUserTier
)

type User struct {
	ID          int       `json:"-"`
	PublicKey   string    `json:"-"`
	UUID        string    `json:"-"`
	Code        string    `json:"user_code"`
	Bandwidth   int       `json:"bw_left"`
	MaxFileSize int       `json:"max_fs"`
	Expiry      time.Time `json:"end_time"`
	MinsAllowed int       `json:"mins_allowed"`
	WantedMins  int       `json:"wanted_mins"`
	Tier        int       `json:"user_tier"`
	Credit      float64   `json:"credit"`
	UUIDKey     string    `json:"UUID_key"`
}

func CreateNewUser(db *sql.DB, user User) {
	_, err := db.Exec(`
	INSERT INTO user (code, UUID, UUID_key, public_key, code_end_dttm, registered_dttm)
	VALUES (?, ?, ?, ?, ?, NOW())`, user.Code, Hash(user.UUID), Hash(user.UUIDKey), user.PublicKey, user.Expiry)
	Handle(err)
}

func UpdateUser(db *sql.DB, user User) {
	Handle(UpdateErr(db.Exec(`
	UPDATE user 
	SET code = ?, public_key = ?, wanted_mins = ?, code_end_dttm = ?
	WHERE UUID=?`, user.Code, user.PublicKey, user.WantedMins, user.Expiry, Hash(user.UUID))))
}

func SetUserUUIDKey(db *sql.DB, user User) {
	Handle(UpdateErr(db.Exec(`
	UPDATE user 
	SET UUID_key = ?
	WHERE UUID=?`, Hash(user.UUIDKey), Hash(user.UUID))))
}

func SetUserTier(db *sql.DB, user *User) {
	SetUserCredit(db, user)
	user.Tier = FreeUserTier
	if user.Credit >= CustomCodeCreditAmt {
		user.Tier = CustomCodeUserTier
	} else if user.Credit >= PermCodeCreditAmt {
		user.Tier = PermUserTier
	} else if user.Credit > 0 {
		user.Tier = PaidUserTier
	}
}

func SetUserMinsAllowed(user *User) {
	if user.Tier == CustomCodeUserTier {
		user.MinsAllowed = 60
	} else if user.Tier == PermUserTier {
		user.MinsAllowed = 30
	} else if user.Tier == PaidUserTier {
		user.MinsAllowed = 20
	}
	user.MinsAllowed = DefaultAccountLifeMins
}

func purgeCode(db *sql.DB, user User) {
	_ = UpdateErr(db.Exec(`UPDATE user
	SET code = NULL, code_end_dttm = NULL
	WHERE UUID = ? AND UUID_key = ?`, user.UUID, user.UUIDKey))
}

func SetUserStats(db *sql.DB, user *User) {
	// get code time left
	SetUserCodeExpiry(db, user)
	if user.Expiry.Sub(time.Now()) <= 0 {
		// user has expired
		go purgeCode(db, *user)
	}

	SetUserMinsAllowed(user)
	SetUserTier(db, user)
	user.Bandwidth = GetUserBandwidthLeft(db, user)
	SetUserMaxFileUpload(db, user)
}

func UserSocketConnected(db *sql.DB, UUID string, connected bool) {
	isConnected := 1
	if !connected {
		isConnected = 0
	}

	Handle(UpdateErr(db.Exec(`UPDATE user
	SET is_connected = ?
	WHERE UUID = ?`, isConnected, Hash(UUID))))
}

func GetUUIDKey(db *sql.DB, user User) (string, bool) {
	var id int
	var key string

	result := db.QueryRow(`
	SELECT UUID_key, id
		FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&key, &id)
	if err == nil && id > 0 {
		return key, true
	}
	return key, false
}

func GetUserUsedBandwidth(db *sql.DB, user User) (bytes int) {
	result := db.QueryRow(`SELECT SUM(size) as used_bandwidth
	FROM transfer
	WHERE from_UUID = ? 
	AND DATE(finished_dttm) = CURDATE()`, Hash(user.UUID))
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

func SetUserMaxFileUpload(db *sql.DB, user *User) {
	SetUserCredit(db, user)
	user.MaxFileSize = CreditToFileUploadSize(user.Credit)
}

func SetUserCodeExpiry(db *sql.DB, user *User) {
	result := db.QueryRow(`SELECT code_end_dttm
	FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&user.Expiry)
	if err != nil || user.Expiry.IsZero() {
		user.Expiry = time.Now().UTC()
	}
}

func CodeToUser(db *sql.DB, code string) (user User) {
	if len(code) != CodeLen {
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
	var letters = []rune("ABCDEFGHJKMNPQRSTUVWXYZ23456789")

	for {
		b := make([]rune, CodeLen)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
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

func GetAllDisplayUsers(db *sql.DB) []DisplayUser {
	var users []DisplayUser
	u, found := c.Get("users")
	if found {
		users = u.([]DisplayUser)
	} else {
		log.Println("Refreshed user cache")
		// fetch users from db if not in cache
		rows, err := db.Query(`SELECT UUID, public_key, is_connected FROM user`)
		defer rows.Close()
		Handle(err)
		for rows.Next() {
			var u DisplayUser
			err = rows.Scan(&u.UUID, &u.PubKey, &u.Connected)
			Handle(err)
			users = append(users, u)
		}
		c.Set("users", users, cache.DefaultExpiration)
	}
	return users
}
