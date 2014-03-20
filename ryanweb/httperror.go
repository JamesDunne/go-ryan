package main

import (
	"log"
	"net/http"
)

type HttpError struct {
	StatusCode  int
	UserMessage string
	TheError    error
}

func NewHttpError(status int, userMessage string, err error) HttpError {
	return HttpError{StatusCode: status, UserMessage: userMessage, TheError: err}
}

func (e HttpError) Error() string {
	return e.TheError.Error()
}

func (e HttpError) String() string {
	return e.UserMessage
}

type ErrorHandler struct {
	handler http.HandlerFunc
}

func NewErrorHandler(handler http.HandlerFunc) ErrorHandler {
	return ErrorHandler{handler: handler}
}

func (h ErrorHandler) ServeHTTP(rsp http.ResponseWriter, req *http.Request) {
	// Catch any panics from the handler:
	pnk := try(func() {
		h.handler(rsp, req)
	})

	// Log errors and return desired HTTP status code:
	if pnk != nil {
		if herr, ok := pnk.(HttpError); ok {
			log.Printf("ERROR: %s\n", herr.Error())
			http.Error(rsp, herr.UserMessage, herr.StatusCode)
		} else if err, ok := pnk.(error); ok {
			log.Printf("ERROR: %s\n", err.Error())
			http.Error(rsp, "500 Internal Server Error", http.StatusInternalServerError)
		} else {
			log.Printf("ERROR: %s\n", pnk)
			http.Error(rsp, "500 Internal Server Error", http.StatusInternalServerError)
		}
		return
	}
}
