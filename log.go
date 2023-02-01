// Logs

package main

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

var LOG_MUTEX = sync.Mutex{}
var LOG_REQUESTS_ENABLED = true
var LOG_DEBUG_ENABLED = false

// Loads log configuration
func InitLog() {
	LOG_REQUESTS_ENABLED = (os.Getenv("LOG_REQUESTS") != "NO")
	LOG_DEBUG_ENABLED = os.Getenv("LOG_DEBUG") == "YES"
}

// Logs a message, including the date and time
func LogLine(line string) {
	tm := time.Now()
	LOG_MUTEX.Lock()
	defer LOG_MUTEX.Unlock()
	fmt.Printf("[%s] %s\n", tm.Format("2006-01-02 15:04:05"), line)
}

// Logs a warning message
func LogWarning(line string) {
	LogLine("[WARNING] " + line)
}

// Logs an info message
func LogInfo(line string) {
	LogLine("[INFO] " + line)
}

// Logs an error message
func LogError(err error) {
	LogLine("[ERROR] " + err.Error())
}

// Logs a request message
func LogRequest(session_id uint64, ip string, line string) {
	if LOG_REQUESTS_ENABLED {
		LogLine("[REQUEST] #" + strconv.Itoa(int(session_id)) + " (" + ip + ") " + line)
	}
}

// Logs a debug message
func LogDebug(line string) {
	if LOG_DEBUG_ENABLED {
		LogLine("[DEBUG] " + line)
	}
}

// Logs a debug message for a session
func LogDebugSession(session_id uint64, ip string, line string) {
	if LOG_DEBUG_ENABLED {
		LogLine("[DEBUG] #" + strconv.Itoa(int(session_id)) + " (" + ip + ") " + line)
	}
}
