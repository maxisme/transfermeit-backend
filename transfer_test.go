package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/minio/minio-go/v7"
	"testing"
	"time"
)

func TestCleanExpiredTransfers(t *testing.T) {
	// create two users
	user1, form1 := createUser()
	user2, form2 := createUser()
	_, _, user2Ws, _ := connectWSS(user2, form2)

	fileSize := MegabytesToBytes(10)
	_ = upload(t, user1, user2, form1, fileSize)

	message := readSocketMessage(user2Ws)
	objectName := message.ObjectPath
	user2Ws.Close()

	// delete expired transfers (there shouldn't be any)
	_ = s.CleanExpiredTransfers(nil)

	// verify upload file still exists after cleanup
	obj, _ := s.minio.GetObject(context.Background(), bucketName, objectName, minio.GetObjectOptions{})
	stat, _ := obj.Stat()
	if stat.Size == 0 {
		t.Errorf("object at path: '%v' should exist - %v", objectName, stat)
		return
	}

	// force transfer to appear expired
	markTransferAsExpired(user2.UUID)

	_ = s.CleanExpiredTransfers(nil)

	// verify upload file has been deleted
	obj, _ = s.minio.GetObject(context.Background(), bucketName, objectName, minio.GetObjectOptions{})
	stat, _ = obj.Stat()
	if stat.Size > 0 {
		t.Errorf("object at path: '%v' should have been deleted - %v", objectName, stat)
		return
	}
}

func TestKeepAliveTransfer(t *testing.T) {
	// create two users
	user1, form1 := createUser()
	user2, form2 := createUser()
	_, _, user2Ws, _ := connectWSS(user2, form2)

	// UPLOAD
	fileSize := MegabytesToBytes(10)
	_ = upload(t, user1, user2, form1, fileSize)

	timeBeforeKeepAlive := getTransferUpdateDttm(user2.UUID)

	message := readSocketMessage(user2Ws)
	time.Sleep(1 * time.Second)
	socketMessage, _ := json.Marshal(ClientSocketMessage{Type: "keep-alive", Content: message.ObjectPath})
	_ = user2Ws.WriteMessage(websocket.TextMessage, socketMessage)
	time.Sleep(50 * time.Millisecond) // needed to wait for socket message to be processed

	// difference of update_dttm before and after keep alive
	difference := getTransferUpdateDttm(user2.UUID).Sub(timeBeforeKeepAlive).Seconds()
	if difference != 1 {
		t.Errorf("%v", difference)
	}
}

func TestInvalidTransferSize(t *testing.T) {

}

func TestNoUploadCookie(t *testing.T) {

}

func TestTransferToOfflineFriend(t *testing.T) {

}

func TestDeleteFileMidTransfer(t *testing.T) {

}
