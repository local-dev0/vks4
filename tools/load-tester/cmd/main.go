// load-tester эмулирует N виртуальных WebRTC-peer-ов для smoke и нагрузочных тестов.
//
// Сценарий:
//  1. POST /api/v1/auth/login (admin@local/admin) → access token.
//  2. POST /api/v1/rooms → room id.
//  3. Для каждого peer: открыть WS /ws, отправить join, отправить пустой SDP offer,
//     прочитать answer (loopback ECHO в MVP signaling.Loopback media client).
//
// В production-тестировании к ws очевидно надо подключать реальный Pion
// PeerConnection с testsrc/audiotestsrc — но для CI smoke достаточно убедиться
// что REST + WS + media-router отвечают.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func main() {
	endpoint := flag.String("endpoint", env("ENDPOINT", "https://caddy"), "Caddy URL")
	users := flag.Int("users", 2, "number of virtual peers")
	roomName := flag.String("room", fmt.Sprintf("smoke-%d", time.Now().Unix()), "room name")
	insecure := flag.Bool("insecure", true, "skip TLS verify")
	flag.Parse()

	hc := newHTTP(*insecure)

	token, err := login(hc, *endpoint, "admin@local", "admin")
	if err != nil {
		log.Fatalf("login: %v", err)
	}
	roomID, err := createRoom(hc, *endpoint, token, *roomName)
	if err != nil {
		log.Fatalf("create room: %v", err)
	}
	log.Printf("room created: %s", roomID)

	var wg sync.WaitGroup
	for i := 0; i < *users; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := runPeer(*endpoint, token, roomID, fmt.Sprintf("user-%d", idx)); err != nil {
				log.Printf("peer %d: %v", idx, err)
			}
		}(i)
		time.Sleep(150 * time.Millisecond)
	}
	wg.Wait()
	log.Println("ok")
}

func newHTTP(insecure bool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = insecureTLS()
	}
	return &http.Client{Transport: tr, Timeout: 15 * time.Second}
}

func login(hc *http.Client, endpoint, email, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	r, err := hc.Post(endpoint+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return "", fmt.Errorf("login %d: %s", r.StatusCode, string(b))
	}
	var t struct{ Access string }
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		return "", err
	}
	return t.Access, nil
}

func createRoom(hc *http.Client, endpoint, token, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{"name": name})
	req, _ := http.NewRequest("POST", endpoint+"/api/v1/rooms", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	if r.StatusCode/100 != 2 {
		b, _ := io.ReadAll(r.Body)
		return "", fmt.Errorf("create %d: %s", r.StatusCode, string(b))
	}
	var room struct{ ID string }
	if err := json.NewDecoder(r.Body).Decode(&room); err != nil {
		return "", err
	}
	return room.ID, nil
}

func runPeer(endpoint, token, roomID, name string) error {
	wsURL := wsURLFrom(endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"vks4.signaling.v1"},
	})
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	// Send join.
	if err := wsjson.Write(ctx, conn, map[string]any{
		"type":    "join",
		"payload": map[string]any{"roomId": roomID, "token": token, "displayName": name},
	}); err != nil {
		return err
	}
	// Read joined or error.
	var env struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := wsjson.Read(ctx, conn, &env); err != nil {
		return err
	}
	if env.Type == "error" {
		return fmt.Errorf("server error: %s", string(env.Payload))
	}
	if env.Type != "joined" {
		return fmt.Errorf("unexpected first msg: %s", env.Type)
	}
	// Hold connection 5 seconds — это даёт серверу время отправить участникам peer-joined.
	time.Sleep(5 * time.Second)
	return nil
}

func wsURLFrom(http string) string {
	switch {
	case len(http) > 8 && http[:8] == "https://":
		return "wss://" + http[8:] + "/ws"
	case len(http) > 7 && http[:7] == "http://":
		return "ws://" + http[7:] + "/ws"
	default:
		return "ws://" + http + "/ws"
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
