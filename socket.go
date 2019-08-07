package main

type Message struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type SocketMessage struct {
	User     *User    `json:"user"`
	Download *Upload  `json:"download"`
	Message  *Message `json:"message"`
}
