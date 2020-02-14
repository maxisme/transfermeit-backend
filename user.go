package main

import (
	"database/sql"
	"github.com/patrickmn/go-cache"
	"log"
	"time"
)

const codeLen = 7
const keyUUIDLen = 200
const (
	defaultAccountLifeMins = 10
	maxAccountLifeMins     = 60
)
const (
	freeUserTier       = 0
	paidUserTier       = 1
	permUserTier       = 2
	customCodeUserTier = 3
)

const (
	permCodeCreditAmt   = 5.0  // once user has this much credit they will have PermUserTier
	customCodeCreditAmt = 10.0 // once user has this much credit they will have CustomCodeUserTier
)

// User structure
type User struct {
	ID            int       `json:"-"`
	PublicKey     string    `json:"-"`
	UUID          string    `json:"-"`
	Code          string    `json:"user_code"`
	BandwidthLeft int       `json:"bw_left"`
	MaxFileSize   int       `json:"max_fs"`
	Expiry        time.Time `json:"end_time"`
	MinsAllowed   int       `json:"mins_allowed"`
	WantedMins    int       `json:"wanted_mins"`
	Tier          int       `json:"user_tier"`
	Credit        float64   `json:"credit"`
	UUIDKey       string    `json:"UUID_key"`
}

// Store stores the permanent parts of the User struct in the database
func (user User) Store(db *sql.DB) {
	_, err := db.Exec(`
	INSERT INTO user (code, UUID, UUID_key, public_key, code_end_dttm, registered_dttm)
	VALUES (?, ?, ?, ?, ?, NOW())`, user.Code, Hash(user.UUID), Hash(user.UUIDKey), user.PublicKey, user.Expiry)
	Handle(err)
}

// Update updates the permanent parts of the User struct in the database
func (user User) Update(db *sql.DB) {
	Handle(UpdateErr(db.Exec(`
	UPDATE user 
	SET code = ?, public_key = ?, wanted_mins = ?, code_end_dttm = ?
	WHERE UUID=?`, user.Code, user.PublicKey, user.WantedMins, user.Expiry, Hash(user.UUID))))
}

// UpdateUUIDKey updates the UUID Key of a user
func (user User) UpdateUUIDKey(db *sql.DB) {
	Handle(UpdateErr(db.Exec(`
	UPDATE user 
	SET UUID_key = ?
	WHERE UUID=?`, Hash(user.UUIDKey), Hash(user.UUID))))
}

// GetTier fetches the user tier level
func (user *User) GetTier(db *sql.DB) {
	user.getCredit(db)
	user.Tier = freeUserTier
	if user.Credit >= customCodeCreditAmt {
		user.Tier = customCodeUserTier
	} else if user.Credit >= permCodeCreditAmt {
		user.Tier = permUserTier
	} else if user.Credit > 0 {
		user.Tier = paidUserTier
	}
}

// GetMinsAllowed gets the max minutes of account life the user can have
func (user *User) GetMinsAllowed(db *sql.DB) {
	user.GetTier(db)
	if user.Tier == customCodeUserTier {
		user.MinsAllowed = 60
	} else if user.Tier == permUserTier {
		user.MinsAllowed = 30
	} else if user.Tier == paidUserTier {
		user.MinsAllowed = 20
	} else {
		user.MinsAllowed = defaultAccountLifeMins
	}
}

// GetStats fetches all stored stats of a user
func (user *User) GetStats(db *sql.DB) {
	// get code time left
	user.GetExpiry(db)
	if user.Expiry.Sub(time.Now()) <= 0 {
		// user has expired
		go purgeCode(db, *user)
	}

	user.GetMinsAllowed(db)
	user.GetTier(db)
	user.GetBandwidthLeft(db)
	user.GetMaxFileSize(db)
}

// SetWantedMins sets the WantedMins as long as the request is legitimate
func (user User) SetWantedMins(db *sql.DB, wantedMins int) {
	user.GetMinsAllowed(db)
	if wantedMins <= 0 || wantedMins%5 != 0 || wantedMins > maxAccountLifeMins || wantedMins > user.MinsAllowed {
		user.WantedMins = defaultAccountLifeMins
	} else {
		user.WantedMins = wantedMins
	}
}

// GetUUIDKey will get the UUID_key of a user as well as verifying whether the user exists
func (user User) GetUUIDKey(db *sql.DB) (string, bool) {
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

func (user *User) getCredit(db *sql.DB) {
	if user.Credit > 0 {
		// already set the users credit so don't bother trying again
		return
	}
	credit := GetCredit(db, *user)
	if credit.Valid {
		user.Credit = credit.Float64
	} else {
		user.Credit = 0
	}
}

// GetBandwidthLeft fetches the amount of bandwidth the user has left for today
func (user *User) GetBandwidthLeft(db *sql.DB) {
	user.getCredit(db)
	usedBandwidth := getUserUsedBandwidth(db, *user)
	creditedBandwidth := CreditToBandwidth(user.Credit)
	user.BandwidthLeft = creditedBandwidth - usedBandwidth
}

// GetMaxFileSize fetches the maximum file size a user can upload with
func (user *User) GetMaxFileSize(db *sql.DB) {
	user.getCredit(db)
	user.MaxFileSize = CreditToFileUploadSize(user.Credit)
}

// GetExpiry fetches the expiry date of the current code
func (user *User) GetExpiry(db *sql.DB) {
	result := db.QueryRow(`SELECT code_end_dttm
	FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&user.Expiry)
	if err != nil || user.Expiry.IsZero() {
		user.Expiry = time.Now().UTC()
	}
}

// IsValid returns true if user User has valid credentials else false
func (user User) IsValid(db *sql.DB) bool {
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

// GetWantedMins fetches the amount of minutes the user wants their code to last for
func (user *User) GetWantedMins(db *sql.DB) {
	result := db.QueryRow(`SELECT wanted_mins
	FROM user
	WHERE UUID = ?`, Hash(user.UUID))
	err := result.Scan(&user.WantedMins)
	Handle(err)
}

func getAllDisplayUsers(db *sql.DB) []displayUser {
	var users []displayUser
	u, found := c.Get("users")
	if found {
		users = u.([]displayUser)
	} else {
		log.Println("Refreshed user cache")
		// fetch users from db if not in cache
		rows, err := db.Query(`SELECT UUID, public_key, is_connected FROM user`)
		defer rows.Close()
		Handle(err)
		for rows.Next() {
			var u displayUser
			err = rows.Scan(&u.UUID, &u.PubKey, &u.Connected)
			Handle(err)
			users = append(users, u)
		}
		c.Set("users", users, cache.DefaultExpiration)
	}
	return users
}

// CodeToUser converts a users code to a User.
func CodeToUser(db *sql.DB, code string) (user User) {
	if len(code) != codeLen {
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

// SetUsersPermCode sets the users perm code as long as it matches what they have passed
func SetUsersPermCode(db *sql.DB, user *User, expectedPermCode string) {
	permCode, customCode := GetUserPermCode(db, *user)
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

// IsConnected will store if a user is connected to socket depending on `connected`
func (user User) IsConnected(db *sql.DB, connected bool) {
	isConnected := 1
	if !connected {
		isConnected = 0
	}

	Handle(UpdateErr(db.Exec(`UPDATE user
	SET is_connected = ?
	WHERE UUID = ?`, isConnected, Hash(user.UUID))))
}

func purgeCode(db *sql.DB, user User) {
	_ = UpdateErr(db.Exec(`UPDATE user
	SET code = NULL, code_end_dttm = NULL
	WHERE UUID = ? AND UUID_key = ?`, user.UUID, user.UUIDKey))
}

func codeExists(db *sql.DB, code string) bool {
	var id int
	result := db.QueryRow(`SELECT id
	FROM user
	WHERE code = ?`, code)
	err := result.Scan(&id)
	return err == nil && id > 0
}

func getUserUsedBandwidth(db *sql.DB, user User) (bytes int) {
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
