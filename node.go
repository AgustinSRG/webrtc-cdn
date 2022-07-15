// WebRTC CDN Node

package main

import (
	"crypto/tls"
	"os"
	"strconv"
	"sync"

	"net/http"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
)

// WebRTC_CDN_Node - Status data of the server
// Controls requests and interactions between
// sources, inks, relays and senders
type WebRTC_CDN_Node struct {
	// Config
	id           string
	redisClient  *redis.Client
	standAlone   bool
	upgrader     *websocket.Upgrader
	reqCount     uint64
	sinkCount    uint64
	ipLimit      uint32
	requestLimit uint32

	// Sync
	mutexReqCount *sync.Mutex

	mutexIpCount     *sync.Mutex
	mutexConnections *sync.Mutex

	mutexRedisSend *sync.Mutex

	mutexSinkCount *sync.Mutex

	mutexStatus *sync.Mutex

	// Status
	connections map[uint64]*Connection_Handler
	ipCount     map[string]uint32

	sources map[string]*WRTC_Source
	relays  map[string]*WRTC_Relay

	sinks   map[string]map[uint64]*WRTC_Sink
	senders map[string]map[string]*WRTC_Source_Sender
}

func (node *WebRTC_CDN_Node) init() {
	// Mutex
	node.mutexReqCount = &sync.Mutex{}
	node.mutexIpCount = &sync.Mutex{}
	node.mutexConnections = &sync.Mutex{}
	node.mutexRedisSend = &sync.Mutex{}
	node.mutexStatus = &sync.Mutex{}
	node.mutexSinkCount = &sync.Mutex{}

	// Status
	node.connections = make(map[uint64]*Connection_Handler)
	node.ipCount = make(map[string]uint32)
	node.sources = make(map[string]*WRTC_Source)
	node.relays = make(map[string]*WRTC_Relay)
	node.sinks = make(map[string]map[uint64]*WRTC_Sink)
	node.senders = make(map[string]map[string]*WRTC_Source_Sender)

	// Config
	node.ipLimit = 4
	custom_ip_limit := os.Getenv("MAX_IP_CONCURRENT_CONNECTIONS")
	if custom_ip_limit != "" {
		cil, e := strconv.Atoi(custom_ip_limit)
		if e != nil {
			node.ipLimit = uint32(cil)
		}
	}

	node.reqCount = 0
	node.sinkCount = 0

	node.requestLimit = 100
	custom_req_limit := os.Getenv("MAX_REQUESTS_PER_SOCKET")
	if custom_req_limit != "" {
		cil, e := strconv.Atoi(custom_req_limit)
		if e != nil {
			node.requestLimit = uint32(cil)
		}
	}

	node.standAlone = os.Getenv("STAND_ALONE") == "YES"
}

// Runs the node
func (node *WebRTC_CDN_Node) run() {
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

	if !node.standAlone {
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
