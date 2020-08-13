package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/maxisme/notifi-backend/ws"
	"github.com/minio/minio-go/v7"
	"testing"
	"time"
)

func TestCleanExpiredTransfers(t *testing.T) {
	// create two users
	user1, form1 := createUser()
	user2, form2 := createUser()
	_, _, user1Ws, _ := connectWSS(user1, form1)
	funnel := &ws.Funnel{
		Key:    user1.UUID,
		WSConn: user1Ws,
		PubSub: s.redis.Subscribe(user1.UUID),
	}
	s.funnels.Add(s.redis, funnel)
	_, _, user2Ws, _ := connectWSS(user2, form2)
	funnel2 := &ws.Funnel{
		Key:    user2.UUID,
		WSConn: user2Ws,
		PubSub: s.redis.Subscribe(user2.UUID),
	}
	s.funnels.Add(s.redis, funnel2)
	defer s.funnels.Remove(funnel)
	defer s.funnels.Remove(funnel2)

	fileSize := MegabytesToBytes(10)
	_ = upload(t, user1, user2, form1, fileSize)

	message := readSocketMessage(funnel2.WSConn)
	objectName := message.ObjectPath
	user2Ws.Close()

	// delete expired transfers (there shouldn't be any)
	if err := s.CleanExpiredTransfers(nil); err != nil {
		t.Errorf("failed to clean %v", err)
	}

	// verify upload file still exists after cleanup
	obj, _ := s.minio.GetObject(context.Background(), bucketName, objectName, minio.GetObjectOptions{})
	stat, _ := obj.Stat()
	if stat.Size == 0 {
		t.Errorf("object at path: '%v' should exist - %v", objectName, stat)
		return
	}

	// force transfer to appear expired
	markTransferAsExpired(user2.UUID)

	if err := s.CleanExpiredTransfers(nil); err != nil {
		t.Errorf("failed to clean %v", err)
	}

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
	_, _, user1Ws, _ := connectWSS(user1, form1)
	funnel := &ws.Funnel{
		Key:    user1.UUID,
		WSConn: user1Ws,
		PubSub: s.redis.Subscribe(user1.UUID),
	}
	s.funnels.Add(s.redis, funnel)
	_, _, user2Ws, _ := connectWSS(user2, form2)
	funnel2 := &ws.Funnel{
		Key:    user2.UUID,
		WSConn: user2Ws,
		PubSub: s.redis.Subscribe(user2.UUID),
	}
	s.funnels.Add(s.redis, funnel2)
	defer s.funnels.Remove(funnel)
	defer s.funnels.Remove(funnel2)

	// UPLOAD
	fileSize := MegabytesToBytes(10)
	_ = upload(t, user1, user2, form1, fileSize)

	timeBeforeKeepAlive := getTransferUpdateDttm(user2.UUID)

	message := readSocketMessage(funnel2.WSConn)
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

func LieAboutTransferSize(t *testing.T) {

}
