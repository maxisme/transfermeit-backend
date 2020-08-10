package main

import (
	"encoding/json"
	"github.com/maxisme/notifi-backend/ws"
	log "github.com/sirupsen/logrus"
	"sync"

	"net/http"
)

// DesktopMessage structure
type DesktopMessage struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// SocketMessage structure
type SocketMessage struct {
	User     *User           `json:"user"`
	Download *Transfer       `json:"download"`
	Message  *DesktopMessage `json:"message"`
}

// IncomingSocketMessage structure
type IncomingSocketMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type PendingMessagesMutex struct {
	messages map[string][]SocketMessage
	sync.RWMutex
}

var PendingMessages = PendingMessagesMutex{make(map[string][]SocketMessage), sync.RWMutex{}}

// WSHandler is the http handler for web socket connections
func (s *Server) WSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		WriteError(w, r, http.StatusBadRequest, "Method not allowed")
		return
	}

	var user = User{
		UUID:    r.Header.Get("UUID"),
		UUIDKey: r.Header.Get("UUID-key"),
	}

	if !user.IsValid(s.db) {
		WriteError(w, r, http.StatusBadRequest, "Invalid credentials")
		return
	}

	// validate inputs
	if !IsValidVersion(r.Header.Get("Version")) {
		WriteError(w, r, http.StatusBadRequest, "Invalid version")
		return
	}

	// connect to socket
	WSConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	// initialise funnel
	funnel := &ws.Funnel{
		Key:    Hash(user.UUID),
		WSConn: WSConn,
		PubSub: s.redis.Subscribe(Hash(user.UUID)),
	}
	s.funnels.Add(s.redis, funnel)

	// mark user as connected in db
	go user.IsConnected(s.db, true)

	// incoming socket messages
	for {
		_, message, err := WSConn.ReadMessage()
		if err != nil {
			break
		}

		var mess IncomingSocketMessage
		if err := json.Unmarshal(message, &mess); err != nil {
			Log(r, log.ErrorLevel, err.Error())
		} else {
			if mess.Type == "keep-alive" {
				if err := KeepAliveTransfer(s.db, user, mess.Content); err != nil {
					Log(r, log.ErrorLevel, err.Error())
				}
			} else if mess.Type == "stats" {
				user.SetStats(s.db)
				if err := WSConn.WriteJSON(SocketMessage{
					User: &user,
				}); err != nil {
					Log(r, log.ErrorLevel, err.Error())
				}
			}
		}
	}

	// mark user as disconnected
	go user.IsConnected(s.db, false)

	// remove client from clients
	err = s.funnels.Remove(funnel)
	if err != nil {
		Log(r, log.WarnLevel, err.Error())
	}
}
