package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var mtx sync.Mutex
var money = atomic.Int64{}
var bank = atomic.Int64{}

func main() {
	money.Add(1000)

	payHandlerChain := RequestIDMiddleware(
		LoggingMiddleware(
			http.HandlerFunc(payHandler),
		),
	)
	saveHandlerChain := RequestIDMiddleware(
		LoggingMiddleware(
			http.HandlerFunc(saveHandler),
		),
	)

	http.Handle("/pay", payHandlerChain)
	http.Handle("/save", saveHandlerChain)

	fmt.Println("Server starting on port 9097...")
	err := http.ListenAndServe(":9097", nil)
	if err != nil {
		fmt.Println("HTTP server error", err.Error())
	}
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := GetRequestID(r.Context())

		fmt.Printf("[%s] Start %s %s\n", requestID, r.Method, r.URL.Path)

		rw := &responseWriter{ResponseWriter: w, status: 200}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		fmt.Printf("[%s] End %s %s - status: %d, duration: %v\n",
			requestID, r.Method, r.URL.Path, rw.status, duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func payHandler(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		msg := "error read HTTP body" + err.Error()
		fmt.Printf("[%s] %s\n", requestID, msg)
		w.Write([]byte(msg))
		return
	}

	reqBodyString := string(reqBody)
	reqBodyInt, err := strconv.Atoi(reqBodyString)
	if err != nil {
		fmt.Printf("[%s] Parse error: %v\n", requestID, err)
		w.Write([]byte("invalid amount"))
		return
	}

	mtx.Lock()
	defer mtx.Unlock()

	if money.Load() >= int64(reqBodyInt) {
		money.Add(int64(-reqBodyInt))

		fmt.Printf("[%s] Payment successful: %d, new balance: %d\n",
			requestID, reqBodyInt, money.Load())

		valueMoney := strconv.Itoa(int(money.Load()))
		valuebank := strconv.Itoa(int(bank.Load()))

		response := fmt.Sprintf("current balance: %s, current bank: %s",
			valueMoney, valuebank)
		w.Write([]byte(response))
	} else {
		fmt.Printf("[%s] Low balance: tried %d, have %d\n",
			requestID, reqBodyInt, money.Load())
		w.Write([]byte("low balance"))
	}
}

func saveHandler(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		msg := "error read HTTP body" + err.Error()
		fmt.Printf("[%s] %s\n", requestID, msg)
		w.Write([]byte(msg))
		return
	}

	reqBodyString := string(reqBody)
	reqBodyInt, err := strconv.Atoi(reqBodyString)
	if err != nil {
		fmt.Printf("[%s] Parse error: %v\n", requestID, err)
		w.Write([]byte("invalid amount"))
		return
	}

	mtx.Lock()
	defer mtx.Unlock()

	if money.Load() >= int64(reqBodyInt) {
		money.Add(int64(-reqBodyInt))
		bank.Add(int64(reqBodyInt))

		fmt.Printf("[%s] Transfer successful: %d, new balance: %d, bank: %d\n",
			requestID, reqBodyInt, money.Load(), bank.Load())

		valueMoney := strconv.Itoa(int(money.Load()))
		valuebank := strconv.Itoa(int(bank.Load()))

		response := fmt.Sprintf("current balance: %s, current bank: %s",
			valueMoney, valuebank)
		w.Write([]byte(response))
	} else {
		fmt.Printf("[%s] Low balance for transfer: tried %d, have %d\n",
			requestID, reqBodyInt, money.Load())
		w.Write([]byte("low balance for bank transfer"))
	}
}
