package main

import (
	"encoding/json"
	"net/http"
)

type DesktopMessage struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type SocketMessage struct {
	User     *User           `json:"user"`
	Download *Transfer       `json:"download"`
	Message  *DesktopMessage `json:"message"`
}

type IncomingSocketMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (s *Server) WSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		WriteError(w, r, 400, "Method not allowed")
		return
	}

	// validate inputs
	if !IsValidVersion(r.Header.Get("Version")) {
		WriteError(w, r, 400, "Invalid Version")
		return
	}

	var user = User{
		UUID:    r.Header.Get("UUID"),
		UUIDKey: r.Header.Get("UUID-key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		WriteError(w, r, 401, "Invalid credentials!")
		return
	}

	// connect to socket
	wsconn, _ := Upgrader.Upgrade(w, r, nil)

	// add web socket connection to list of clients
	WSClientsMutex.Lock()
	WSClients[Hash(user.UUID)] = wsconn
	WSClientsMutex.Unlock()

	// mark user as connected in db
	go UserSocketConnected(s.db, r.Header.Get("UUID"), true)

	// get pending messages
	PendingSocketMutex.RLock()
	messages, ok := PendingSocketMessages[Hash(user.UUID)]
	PendingSocketMutex.RUnlock()
	if ok {
		// send pending messages
		for _, message := range messages {
			SendSocketMessage(message, Hash(user.UUID), false)
		}

		// delete any pending socket messages
		PendingSocketMutex.Lock()
		delete(PendingSocketMessages, Hash(user.UUID))
		PendingSocketMutex.Unlock()
	}

	// incoming socket messages
	for {
		_, message, err := wsconn.ReadMessage()
		if err != nil {
			break
		}

		var mess IncomingSocketMessage
		Handle(json.Unmarshal(message, &mess))
		if mess.Type == "keep-alive" {
			Handle(KeepAliveTransfer(s.db, user, mess.Content))
		} else if mess.Type == "stats" {
			SetUserStats(s.db, &user)
			SendSocketMessage(SocketMessage{
				User: &user,
			}, user.UUID, true)
		}
		break
	}

	// mark user as disconnected
	go UserSocketConnected(s.db, r.Header.Get("UUID"), false)

	// remove client from clients
	WSClientsMutex.Lock()
	delete(WSClients, user.UUID)
	WSClientsMutex.Unlock()
}
