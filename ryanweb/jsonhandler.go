package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type JsonHandlerFunc func(*http.Request) interface{}

type JsonHandler struct {
	handler JsonHandlerFunc
}

func NewJsonHandler(handler JsonHandlerFunc) JsonHandler {
	return JsonHandler{handler: handler}
}

func (h JsonHandler) ServeHTTP(rsp http.ResponseWriter, req *http.Request) {
	var rsperr error = nil
	var result interface{}

	// Try to run the handler logic and catch any panics:
	pnk := try(func() {
		result = h.handler(req)
	})
	// Handle the panic and convert it into an `error`:
	if pnk != nil {
		var ok bool
		if rsperr, ok = pnk.(error); !ok {
			// Format the panic as a string if it's not an `error`:
			rsperr = fmt.Errorf("%v", pnk)
		}
	}

	// We're guaranteed that we want to return a JSON result:
	rsp.Header().Add("Content-Type", "application/json; charset=utf-8")

	// Report the application error:
	if rsperr != nil {
		// Error response:
		rsp.WriteHeader(http.StatusInternalServerError)
		bytes, _ := json.Marshal(struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{
			Success: false,
			Message: rsperr.Error(),
		})
		rsp.Write(bytes)
		return
	}

	// Marshal the successful response to JSON:
	//bytes, err := json.Marshal(struct {
	//	Success bool        `json:"success"`
	//	Result  interface{} `json:"result"`
	//}{
	//	Success: false,
	//	Result:  result,
	//})

	bytes, err := json.Marshal(result)
	if err != nil {
		log.Println(err.Error())

		// Canned response:
		rsp.WriteHeader(http.StatusInternalServerError)
		rsp.Write([]byte(`{"success":false,"message":"There was an error attempting to marshal the response object to JSON."}`))
		return
	}

	// Write the marshaled JSON:
	rsp.WriteHeader(http.StatusOK)
	rsp.Write(bytes)
	return
}
