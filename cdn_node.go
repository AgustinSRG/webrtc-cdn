// WebRTC CDN Node

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"net/http"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
)

type WebRTC_CDN_Node struct {
	id          string
	redisClient *redis.Client
	upgrader    *websocket.Upgrader
	reqCount    uint64

	mutexReqCount *sync.Mutex
}

func (node *WebRTC_CDN_Node) init() {
	node.mutexReqCount = &sync.Mutex{}
}

func (node *WebRTC_CDN_Node) getRequestID() uint64 {
	node.mutexReqCount.Lock()
	defer node.mutexReqCount.Unlock()

	node.reqCount++

	return node.reqCount
}

func (node *WebRTC_CDN_Node) receiveRedisMessage(msg string) {
	msgData := map[string]string{}

	// Decode message
	json.Unmarshal([]byte(msg), &msgData)

	msgType := strings.ToUpper(msgData["type"])
	msgSource := msgData["src"]

	if msgSource == node.id {
		return // Ignore messages from self
	}

	switch msgType {
	case "RESOLVE":
	case "INFO":
	case "CONNECT":
	case "OFFER":
	case "ANSWER":
	case "CANDIDATE":
	case "CLOSE":
	}
}

func (node *WebRTC_CDN_Node) sendRedisMessage(channel string, msg *map[string]string) {
	b, e := json.Marshal(msg)
	if e != nil {
		LogError(e)
		return
	}
	r := node.redisClient.Publish(context.Background(), channel, string(b))
	if r != nil && r.Err() != nil {
		LogError(r.Err())
	} else {
		LogDebug("[REDIS] [SENT] Channel: " + channel + " | Message: " + string(b))
	}
}

func (node *WebRTC_CDN_Node) runHTTPSecureServer(wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
	}()

	bind_addr := os.Getenv("BIND_ADDRESS")

	// Setup HTTPS server
	var ssl_port int
	ssl_port = 443
	customSSLPort := os.Getenv("SSL_PORT")
	if customSSLPort != "" {
		sslp, e := strconv.Atoi(customSSLPort)
		if e == nil {
			ssl_port = sslp
		}
	}

	certFile := os.Getenv("SSL_CERT")
	keyFile := os.Getenv("SSL_KEY")

	if certFile != "" && keyFile != "" {
		LogInfo("[SSL] Listening on " + bind_addr + ":" + strconv.Itoa(ssl_port))
		errSSL := http.ListenAndServeTLS(bind_addr+":"+strconv.Itoa(ssl_port), certFile, keyFile, node)

		if errSSL != nil {
			LogError(errSSL)
		}
	}
}

func (node *WebRTC_CDN_Node) runHTTPServer(wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
	}()

	bind_addr := os.Getenv("BIND_ADDRESS")

	// Setup RTMP server
	var tcp_port int
	tcp_port = 80
	customTCPPort := os.Getenv("HTTP_PORT")
	if customTCPPort != "" {
		tcpp, e := strconv.Atoi(customTCPPort)
		if e == nil {
			tcp_port = tcpp
		}
	}

	LogInfo("[HTTP] Listening on " + bind_addr + ":" + strconv.Itoa(tcp_port))
	errHTTP := http.ListenAndServe(bind_addr+":"+strconv.Itoa(tcp_port), node)

	if errHTTP != nil {
		LogError(errHTTP)
	}
}

func (node *WebRTC_CDN_Node) run() {
	// Reset request counter
	node.reqCount = 0

	// Setup Redis sender

	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	redisTLS := os.Getenv("REDIS_TLS")

	if redisTLS == "YES" {
		node.redisClient = redis.NewClient(&redis.Options{
			Addr:      redisHost + ":" + redisPort,
			Password:  redisPassword,
			TLSConfig: &tls.Config{},
		})
	} else {
		node.redisClient = redis.NewClient(&redis.Options{
			Addr:     redisHost + ":" + redisPort,
			Password: redisPassword,
		})
	}

	// Setup websocket handler

	node.upgrader = &websocket.Upgrader{}
	node.upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	var wg sync.WaitGroup

	wg.Add(2)

	go node.runHTTPServer(&wg)
	go node.runHTTPSecureServer(&wg)

	wg.Wait()
}

func (node *WebRTC_CDN_Node) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqId := node.getRequestID()

	LogRequest(reqId, req.RemoteAddr, ""+req.Method+" "+req.RequestURI)

	if req.URL.Path == "/ws" {
		c, err := node.upgrader.Upgrade(w, req, nil)
		if err != nil {
			LogError(err)
			return
		}

		handler := Connection_Handler{
			id:         reqId,
			ip:         req.RemoteAddr,
			node:       node,
			connection: c,
		}

		go handler.run()
	} else {
		w.WriteHeader(200)
		fmt.Fprintf(w, "WebRTC-CDN Signaling Server. Connect to /ws for signaling")
	}
}
