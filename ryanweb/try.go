package main

// Attempts to run `attempt` and recovers from any panics, returning the panic object or nil if success.
func try(attempt func()) (panicked interface{}) {
	defer func() {
		if o := recover(); o != nil {
			panicked = o
			return
		}
	}()

	attempt()

	panicked = nil
	return
}
