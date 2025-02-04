package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julienschmidt/httprouter"
)

func TestLogDefault(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)

	getLogLevelChan = make(chan string, 1)
	setLogLevelChan = make(chan string, 1)
	getLogLevelChan <- "any incumbent value will do to avoid blocking on the channel read"

	w := httptest.NewRecorder()
	LogLevelHandler(w, req, []httprouter.Param{
		httprouter.Param{Key: "level", Value: LogDefault},
	},
	)

	if w.Code != 200 {
		t.Fatalf("Want 200 but got %d\n", w.Code)
	}

	state := <-setLogLevelChan
	if state != LogDefault {
		t.Fatalf("Want default but got %s\n", state)
	}
}

func TestLogDebug(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)

	getLogLevelChan = make(chan string, 1)
	setLogLevelChan = make(chan string, 1)
	getLogLevelChan <- "any incumbent value will do to avoid blocking on the channel read"

	w := httptest.NewRecorder()
	LogLevelHandler(w, req, []httprouter.Param{
		httprouter.Param{Key: "level", Value: LogDebug},
	},
	)

	if w.Code != 200 {
		t.Fatalf("Want 200 but got %d\n", w.Code)
	}

	state := <-setLogLevelChan
	if state != LogDebug {
		t.Fatalf("Want debug but got %s\n", state)
	}
}

func TestInvalidLogLevel(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)

	getLogLevelChan = make(chan string, 1)
	setLogLevelChan = make(chan string, 1)
	getLogLevelChan <- "any incumbent value will do to avoid blocking on the channel read"

	w := httptest.NewRecorder()
	LogLevelHandler(w, req, []httprouter.Param{
		httprouter.Param{Key: "level", Value: "nope"},
	},
	)

	if w.Code != 400 {
		t.Fatalf("Want 400 but got %d\n", w.Code)
	}
}
