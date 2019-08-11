package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type DesktopMessage struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type SocketMessage struct {
	User     *User           `json:"user"`
	Download *Upload         `json:"download"`
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

	// CONNECT TO SOCKET
	wsconn, _ := upgrader.Upgrade(w, r, nil)
	clients[Hash(user.UUID)] = wsconn // add conn to clients
	go UserSocketConnected(s.db, user, true)

	// SEND ALL PENDING MESSAGES
	if messages, ok := pendingSocketMessages[Hash(user.UUID)]; ok {
		for _, message := range messages {
			SendSocketMessage(message, Hash(user.UUID), false)
		}
		delete(pendingSocketMessages, Hash(user.UUID)) // delete pending messages
	}

	// INCOMING SOCKET MESSAGES
	for {
		_, message, err := wsconn.ReadMessage()
		if err != nil {
			log.Println(err.Error())
			break
		}

		var mess IncomingSocketMessage
		Handle(json.Unmarshal(message, &mess))
		if mess.Type == "keep-alive" {
			Handle(KeepAliveUpload(s.db, user, mess.Content))
		} else if mess.Type == "stats" {
			SetUserStats(s.db, &user)
			go SendSocketMessage(SocketMessage{
				User: &user,
			}, user.UUID, true)
		}
		break
	}

	go UserSocketConnected(s.db, user, false)
	delete(clients, user.UUID)
}
