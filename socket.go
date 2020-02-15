package main

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
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

type Funnel struct {
	conn *websocket.Conn
	sync.RWMutex
}

type Funnels struct {
	conns map[string]*Funnel
	sync.RWMutex
}

// WSConns stores all connected web sockets
var WSConns = Funnels{make(map[string]*Funnel), sync.RWMutex{}}

var PendingMessages = PendingMessagesMutex{make(map[string][]SocketMessage), sync.RWMutex{}}

// WSHandler is the http handler for web socket connections
func (s *Server) WSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		WriteError(w, r, 400, "Method not allowed")
		return
	}

	var user = User{
		UUID:    r.Header.Get("UUID"),
		UUIDKey: r.Header.Get("UUID-key"),
	}
	UUIDHash := Hash(user.UUID)

	if !user.IsValid(s.db) {
		WriteError(w, r, 401, "Invalid credentials!")
		return
	}

	// validate inputs
	if !IsValidVersion(r.Header.Get("Version")) {
		WriteError(w, r, 400, "Invalid Version")
		return
	}

	// connect to socket
	wsconn, err := upgrader.Upgrade(w, r, nil)
	Handle(err)
	f := Funnel{}
	f.conn = wsconn

	// add web socket connection to list of clients
	WSConns.AddConn(UUIDHash, &f)

	// mark user as connected in db
	go user.IsConnected(s.db, true)

	// write pending messages
	PendingMessages.RLock()
	messages, ok := PendingMessages.messages[UUIDHash]
	PendingMessages.RUnlock()
	if ok {
		// send pending messages to user
		for _, message := range messages {
			go Handle(f.Write(message))
		}
	}

	// delete pending socket messages
	PendingMessages.Lock()
	delete(PendingMessages.messages, UUIDHash)
	PendingMessages.Unlock()

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
			WSConns.Write(SocketMessage{
				User: &user,
			}, user.UUID, true)
		}
		break
	}

	// mark user as disconnected
	go user.IsConnected(s.db, false)

	// remove client from clients
	WSConns.RemConn(UUIDHash)
}

func (funnel *Funnel) Write(message interface{}) error {
	jsonReply, err := json.Marshal(message)
	if err != nil {
		return err
	}
	funnel.Lock()
	err = funnel.conn.WriteMessage(websocket.TextMessage, jsonReply)
	funnel.Unlock()
	return err
}

// Write sends a socket message to a connected UUID and stores if not connected
func (conns *Funnels) Write(message SocketMessage, UUID string, storeOnFail bool) bool {
	hashUUID := Hash(UUID)

	socket, ok := conns.GetConn(hashUUID)
	if ok {
		err := socket.Write(message)
		if err == nil {
			// successfully sent socket message
			return true
		}
		Handle(err)
	} else {
		log.Println("UUID not connected to socket: " + hashUUID)
	}

	if storeOnFail {
		PendingMessages.Lock()
		PendingMessages.messages[hashUUID] = append(PendingMessages.messages[hashUUID], message)
		PendingMessages.Unlock()
	}

	return false
}

func (conns *Funnels) GetConn(key string) (*Funnel, bool) {
	conns.RLock()
	socket, ok := conns.conns[key]
	conns.RUnlock()
	return socket, ok
}

func (conns *Funnels) AddConn(key string, funnel *Funnel) {
	conns.Lock()
	conns.conns[key] = funnel
	conns.Unlock()
}

func (conns *Funnels) RemConn(key string) {
	conns.Lock()
	delete(conns.conns, key)
	conns.Unlock()
}
