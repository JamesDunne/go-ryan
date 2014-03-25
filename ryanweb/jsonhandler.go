package main

import (
	"encoding/json"
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
	var result interface{}

	// We're guaranteed that we want to return a JSON result:
	rsp.Header().Add("Content-Type", "application/json; charset=utf-8")

	// Try to run the handler logic and catch any panics:
	pnk, stackTrace := try(func() {
		result = h.handler(req)
	})

	// Handle the panic:
	if pnk != nil {
		statusCode, userMessage, logError := getErrorDetails(pnk, stackTrace)

		// Log the private error details:
		log.Printf("ERROR: %s\n", logError)

		// Error response:
		rsp.WriteHeader(statusCode)
		bytes, _ := json.Marshal(struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}{
			Success: false,
			Message: userMessage,
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
		log.Printf("There was an error attempting to marshal the response object to JSON; %s\n", err.Error())

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
