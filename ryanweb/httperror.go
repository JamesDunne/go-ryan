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

func getErrorDetails(panicked interface{}, stackTrace string) (statusCode int, userMessage string, logError string) {
	if herr, ok := panicked.(HttpError); ok {
		logError = fmt.Sprintf("%s\n  STACK: %s", herr.Error(), stackTrace)
		userMessage = herr.UserMessage
		statusCode = herr.StatusCode
	} else if err, ok := panicked.(error); ok {
		logError = fmt.Sprintf("%s\n  STACK: %s", err.Error(), stackTrace)
		userMessage = "500 Internal Server Error"
		statusCode = http.StatusInternalServerError
	} else {
		logError = fmt.Sprintf("%s\n STACK: %s", panicked, stackTrace)
		userMessage = "500 Internal Server Error"
		statusCode = http.StatusInternalServerError
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
	pnk, stackTrace := try(func() {
		h.handler(rsp, req)
	})

	// Log errors and return desired HTTP status code:
	if pnk != nil {
		statusCode, userMessage, logError := getErrorDetails(pnk, stackTrace)

		log.Printf("ERROR: %s\n", logError)
		http.Error(rsp, userMessage, statusCode)
		return
	}
}
