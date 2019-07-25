package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"runtime"
)

func Err(err error) {
	pc, _, _, _ := runtime.Caller(1)
	details := runtime.FuncForPC(pc)
	if err != nil {
		log.Fatal(err.Error() + " - " + details.Name())
	}
}

func MegabytesToBytes(megabytes float64) int {
	return int(megabytes * 1000000)
}

func BytesToMegabytes(bytes int) float64 {
	return float64(bytes / 1000000)
}

func CreditToFileUpload(credit float64) (bytes int) {
	bytes = FREEFILEUPLOAD
	cnt := 0
	for {
		if credit > 0 {
			bytes += cnt * FREEFILEUPLOAD
			credit -= CREDITSTEPS
			continue
		}
		break
	}
	return
}

func CreditToBandwidth(credit float64) (bytes int) {
	bytes = FREEBANDWIDTH
	cnt := 0
	for {
		if credit > 0 {
			bytes += cnt * FREEBANDWIDTH
			credit -= CREDITSTEPS
			continue
		}
		break
	}
	return
}

func WriteError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	_, err := w.Write([]byte(message))
	Err(err)
}

// calculate the size of a file after is has been encrypted with aes on the client side with RNCryptor
func FileSizeToRNCryptorBytes(bytes int) int {
	overhead := 66
	if bytes == 0 {
		return overhead + 16
	}
	remainder := bytes % 16
	if remainder == 0 {
		return bytes + 16 + overhead
	}
	return bytes + 16 + overhead - remainder
}

func SendSocketMessage(message SocketMessage, UUID string, storeOnFail bool) bool {
	if socket, ok := clients[UUID]; ok {
		jsonReply, err := json.Marshal(message)
		Err(err)
		if err = socket.WriteMessage(websocket.TextMessage, jsonReply); err == nil {
			// successfully sent socket message
			return true
		} else {
			fmt.Println(err.Error())
		}
		Err(err)
		fmt.Println(err.Error())
	}

	if storeOnFail {
		pendingSocketMessages[UUID] = append(pendingSocketMessages[UUID], message)
	}
	return false
}

func initSession(r *http.Request) *sessions.Session {
	if appSession != nil {
		return appSession
	}
	session, err := store.Get(r, UPLOADSESSIONNAME)
	appSession = session
	Err(err)
	return session
}
