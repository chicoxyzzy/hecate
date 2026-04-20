package gateway

import "errors"

var (
	errDenied = errors.New("request denied")
	errClient = errors.New("client error")
)

func IsDeniedError(err error) bool {
	return errors.Is(err, errDenied)
}

func IsClientError(err error) bool {
	return errors.Is(err, errClient)
}
