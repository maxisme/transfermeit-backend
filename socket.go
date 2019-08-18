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
		WriteError(w, 400, "Method not allowed")
		return
	}

	// validate inputs
	if !IsValidVersion(r.Header.Get("Version")) {
		WriteError(w, 400, "Invalid Version")
		return
	}

	var user = User{
		UUID:    r.Header.Get("UUID"),
		UUIDKey: r.Header.Get("UUID-key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		WriteError(w, 401, "Invalid credentials!")
		return
	}

	// connect to socket
	wsconn, _ := upgrader.Upgrade(w, r, nil)

	// add web socket connection to list of clients
	clientsMutex.Lock()
	clients[Hash(user.UUID)] = wsconn
	clientsMutex.Unlock()

	// mark user as connected in db
	go UserSocketConnected(s.db, user, true)

	// send user info
	SetUserStats(s.db, &user)
	SendSocketMessage(SocketMessage{
		User: &user,
	}, user.UUID, true)

	// get pending messages
	pendingSocketMutex.RLock()
	messages, ok := pendingSocketMessages[Hash(user.UUID)]
	pendingSocketMutex.RUnlock()
	if ok {
		// send pending messages
		for _, message := range messages {
			SendSocketMessage(message, Hash(user.UUID), false)
		}

		// delete pending messages
		pendingSocketMutex.Lock()
		delete(pendingSocketMessages, Hash(user.UUID))
		pendingSocketMutex.Unlock()
	}

	// incoming socket messages
	for {
		_, message, err := wsconn.ReadMessage()
		if err != nil {
			Handle(err)
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

	go UserSocketConnected(s.db, user, false)

	// remove client from clients
	clientsMutex.Lock()
	delete(clients, user.UUID)
	clientsMutex.Unlock()
}
