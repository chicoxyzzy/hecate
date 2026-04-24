package providers

import "net/http"

type testRoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn testRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
