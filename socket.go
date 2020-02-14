package main

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
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

// WSHandler is the http handler for web socket connections
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

	if !user.IsValid(s.db) {
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
	go user.IsConnected(s.db, true)

	// get pending messages
	PendingSocketMutex.RLock()
	messages, ok := PendingSocketMessages[Hash(user.UUID)]
	PendingSocketMutex.RUnlock()
	if ok {
		// send pending messages to user
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
			go KeepAliveTransfer(s.db, user, mess.Content)
		} else if mess.Type == "stats" {
			user.GetStats(s.db)
			SendSocketMessage(SocketMessage{
				User: &user,
			}, user.UUID, true)
		}
		break
	}

	// mark user as disconnected
	go user.IsConnected(s.db, false)

	// remove client from clients
	WSClientsMutex.Lock()
	delete(WSClients, user.UUID)
	WSClientsMutex.Unlock()
}

// SendSocketMessage sends a socket message to a connected UUID and stores if not connected
func SendSocketMessage(message SocketMessage, UUID string, storeOnFail bool) bool {
	hashUUID := Hash(UUID)

	WSClientsMutex.RLock()
	socket, ok := WSClients[hashUUID]
	WSClientsMutex.RUnlock()

	if ok {
		jsonReply, err := json.Marshal(message)
		Handle(err)
		err = socket.WriteMessage(websocket.TextMessage, jsonReply)
		if err == nil {
			// successfully sent socket message
			return true
		}
		Handle(err)
	} else {
		log.Println("UUID not connected to socket: " + hashUUID)
	}

	if storeOnFail {
		PendingSocketMutex.Lock()
		PendingSocketMessages[hashUUID] = append(PendingSocketMessages[hashUUID], message)
		PendingSocketMutex.Unlock()
	}

	return false
}
