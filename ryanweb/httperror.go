package main

import (
	"fmt"
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

func getErrorDetails(panicked interface{}) (statusCode int, userMessage string, logError string) {
	if herr, ok := panicked.(HttpError); ok {
		statusCode = herr.StatusCode
		logError = herr.Error()
		userMessage = herr.UserMessage
	} else if err, ok := panicked.(error); ok {
		statusCode = http.StatusInternalServerError
		logError = err.Error()
		userMessage = "500 Internal Server Error"
	} else {
		statusCode = http.StatusInternalServerError
		logError = fmt.Sprintf("%s", panicked)
		userMessage = "500 Internal Server Error"
	}
	return
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
		statusCode, userMessage, logError := getErrorDetails(pnk)

		log.Printf("ERROR: %s\n", logError)
		http.Error(rsp, userMessage, statusCode)
		return
	}
}
