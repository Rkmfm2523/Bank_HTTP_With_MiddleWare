package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
)

type RequestIDKey string

const (
	RequestIDHeader                  = "X-Request-ID"
	RequestIDContextKey RequestIDKey = "requestID"
)

func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-id"
	}

	return base64.RawURLEncoding.EncodeToString(b)
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" || requestID == " " {
			requestID = generateRequestID()
		}

		ctx := context.WithValue(r.Context(), RequestIDContextKey, requestID)

		r = r.WithContext(ctx)

		w.Header().Set(RequestIDHeader, requestID)

		next.ServeHTTP(w, r)
	})
}

func GetRequestID(ctx context.Context) string {
	if ctx != nil {
		if str, ok := ctx.Value(RequestIDContextKey).(string); ok {
			return str
		}
	}
	return ""
}
