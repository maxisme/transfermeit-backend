package main

type Message struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type SocketMessage struct {
	MessageType string   `json:"message"`
	User        *User    `json:"user"`
	Download    *Upload  `json:"download"`
	Message     *Message `json:"message"`
}

var pendingSocketMessages = map[string][]SocketMessage{}
