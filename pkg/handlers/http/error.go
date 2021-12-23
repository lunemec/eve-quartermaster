package http

import (
	"net/http"

	"go.uber.org/zap"
)

type handlerError struct {
	err  error
	code int
}

func (e handlerError) Error() string {
	return e.err.Error()
}

type errorHandlerFunc func(http.ResponseWriter, *http.Request) error

func ErrorHandler(handler errorHandlerFunc, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := handler(w, r)
		if err != nil {
			log.Error("handler error", zap.Error(err))
			switch v := err.(type) {
			case handlerError:
				http.Error(w, v.Error(), v.code)
				return
			default:
				http.Error(w, v.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
}

func Error(err error, code int) error {
	return &handlerError{err: err, code: code}
}
