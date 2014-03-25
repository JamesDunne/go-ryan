package main

import (
	//"runtime"
	"runtime/debug"
)

// Attempts to run `attempt` and recovers from any panics, returning the panic object or nil if success.
func try(attempt func()) (panicked interface{}, stackTrace string) {
	defer func() {
		if o := recover(); o != nil {
			panicked = o

			stackTrace = string(debug.Stack())

			// NOTE(jsd): This always returns n == 0 on my windows/amd64 system. Untested on other systems.
			// Get stack trace:
			//stkBytes := make([]byte, 0, 16384)
			//n := runtime.Stack(stkBytes, false)
			//stackTrace = string(stkBytes[:n])

			return
		}
	}()

	attempt()

	panicked = nil
	stackTrace = ""
	return
}
