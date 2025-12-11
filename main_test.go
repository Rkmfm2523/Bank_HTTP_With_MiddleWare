package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestIDMiddleware(t *testing.T) {
	tests := []struct {
		name            string
		headerValue     string
		expectGenerated bool
	}{
		{"No Header - Generate New", "", true},
		{"Empty Header - Generate New", " ", true},
		{"Existing Header - Use It", "test-request-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestID := GetRequestID(r.Context())
				if requestID == "" {
					t.Error("RequestID is empty in handler")
				}

				if tt.expectGenerated {
					if len(requestID) < 10 {
						t.Errorf("Generated request ID seems too short: %s", requestID)
					}
				} else {
					if requestID != tt.headerValue {
						t.Errorf("Expected request ID %s, got %s", tt.headerValue, requestID)
					}
				}

				if respID := w.Header().Get(RequestIDHeader); respID != requestID {
					t.Errorf("Response header mismatch: expected %s, got %s", requestID, respID)
				}
			})

			handler := RequestIDMiddleware(testHandler)
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.headerValue != "" {
				req.Header.Set(RequestIDHeader, tt.headerValue)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	oldOutput := logOutput
	defer func() { logOutput = oldOutput }()

	var logMessages []string
	logOutput = func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		logMessages = append(logMessages, msg)
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	handler := RequestIDMiddleware(LoggingMiddleware(testHandler))
	req := httptest.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if len(logMessages) < 2 {
		t.Fatalf("Expected at least 2 log messages, got %d", len(logMessages))
	}

	startLog := logMessages[0]
	endLog := logMessages[1]

	if !strings.Contains(startLog, "Start POST /test") {
		t.Errorf("Start log missing method/path: %s", startLog)
	}

	if !strings.Contains(endLog, "End POST /test") {
		t.Errorf("End log missing method/path: %s", endLog)
	}

	if !strings.Contains(endLog, "status: 200") {
		t.Errorf("End log missing status: %s", endLog)
	}

	if !strings.Contains(endLog, "duration:") {
		t.Errorf("End log missing duration: %s", endLog)
	}
}

func TestPayHandler(t *testing.T) {
	defer func() { money.Store(1000) }()
	money.Store(1000)
	bank.Store(0)

	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
		expectedBody   string
		expectedMoney  int64
		expectedBank   int64
	}{
		{
			name:           "Successful Payment",
			requestBody:    "150",
			expectedStatus: 200,
			expectedBody:   "current balance: 850, current bank: 0",
			expectedMoney:  850,
			expectedBank:   0,
		},
		{
			name:           "Insufficient Funds",
			requestBody:    "1500",
			expectedStatus: 200,
			expectedBody:   "low balance",
			expectedMoney:  1000,
			expectedBank:   0,
		},
		{
			name:           "Invalid Amount Format",
			requestBody:    "not-a-number",
			expectedStatus: 200,
			expectedBody:   "invalid amount",
			expectedMoney:  1000,
			expectedBank:   0,
		},
		{
			name:           "Empty Request Body",
			requestBody:    "",
			expectedStatus: 200,
			expectedBody:   "invalid amount",
			expectedMoney:  1000,
			expectedBank:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			money.Store(1000)
			bank.Store(0)

			req := httptest.NewRequest("POST", "/pay", bytes.NewBufferString(tt.requestBody))
			req = req.WithContext(context.WithValue(req.Context(), RequestIDContextKey, "test-id"))

			w := httptest.NewRecorder()
			payHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			bodyStr := strings.TrimSpace(string(body))

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if bodyStr != tt.expectedBody {
				t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, bodyStr)
			}

			if money.Load() != tt.expectedMoney {
				t.Errorf("Expected money %d, got %d", tt.expectedMoney, money.Load())
			}

			if bank.Load() != tt.expectedBank {
				t.Errorf("Expected bank %d, got %d", tt.expectedBank, bank.Load())
			}
		})
	}
}

func TestSaveHandler(t *testing.T) {
	defer func() { money.Store(1000); bank.Store(0) }()
	money.Store(1000)
	bank.Store(0)

	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
		expectedBody   string
		expectedMoney  int64
		expectedBank   int64
	}{
		{
			name:           "Successful Transfer",
			requestBody:    "200",
			expectedStatus: 200,
			expectedBody:   "current balance: 800, current bank: 200",
			expectedMoney:  800,
			expectedBank:   200,
		},
		{
			name:           "Insufficient Funds for Transfer",
			requestBody:    "1500",
			expectedStatus: 200,
			expectedBody:   "low balance for bank transfer",
			expectedMoney:  1000,
			expectedBank:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			money.Store(1000)
			bank.Store(0)

			req := httptest.NewRequest("POST", "/save", bytes.NewBufferString(tt.requestBody))
			req = req.WithContext(context.WithValue(req.Context(), RequestIDContextKey, "test-id"))

			w := httptest.NewRecorder()
			saveHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			bodyStr := strings.TrimSpace(string(body))

			if bodyStr != tt.expectedBody {
				t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, bodyStr)
			}

			if money.Load() != tt.expectedMoney {
				t.Errorf("Expected money %d, got %d", tt.expectedMoney, money.Load())
			}

			if bank.Load() != tt.expectedBank {
				t.Errorf("Expected bank %d, got %d", tt.expectedBank, bank.Load())
			}
		})
	}
}

func TestConcurrentPayments(t *testing.T) {
	defer func() { money.Store(1000); bank.Store(0) }()

	const (
		initialBalance = 1000
		numRequests    = 50
		paymentAmount  = 10
	)

	money.Store(initialBalance)
	bank.Store(0)

	var wg sync.WaitGroup
	errors := make(chan error, numRequests)
	successfulPayments := atomic.Int32{}

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := httptest.NewRequest("POST", "/pay",
				bytes.NewBufferString(strconv.Itoa(paymentAmount)))
			req = req.WithContext(context.WithValue(req.Context(),
				RequestIDContextKey, fmt.Sprintf("conc-test-%d", id)))

			w := httptest.NewRecorder()
			payHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == 200 {
				if strings.Contains(string(body), "current balance") {
					successfulPayments.Add(1)
				} else if !strings.Contains(string(body), "low balance") {
					errors <- fmt.Errorf("unexpected response: %s", string(body))
				}
			} else {
				errors <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	expectedSuccessful := int32(initialBalance / paymentAmount)
	actualSuccessful := successfulPayments.Load()

	if actualSuccessful > expectedSuccessful {
		t.Errorf("Too many successful payments: %d (max expected: %d)",
			actualSuccessful, expectedSuccessful)
	}

	finalBalance := money.Load()
	if finalBalance < 0 {
		t.Errorf("Balance went negative: %d", finalBalance)
	}

	expectedBalance := initialBalance - int64(actualSuccessful)*paymentAmount
	if finalBalance != expectedBalance {
		t.Errorf("Final balance mismatch: got %d, expected %d (successful payments: %d)",
			finalBalance, expectedBalance, actualSuccessful)
	}
}

func TestResponseWriter(t *testing.T) {
	tests := []struct {
		name           string
		writeFunc      func(w *responseWriter)
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "WriteHeader sets status",
			writeFunc: func(w *responseWriter) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "",
		},
		{
			name: "Write without WriteHeader",
			writeFunc: func(w *responseWriter) {
				w.Write([]byte("test body"))
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "test body",
		},
		{
			name: "WriteHeader then Write",
			writeFunc: func(w *responseWriter) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			rw := &responseWriter{
				ResponseWriter: recorder,
				status:         http.StatusOK,
			}

			tt.writeFunc(rw)

			if rw.status != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rw.status)
			}

			if body := recorder.Body.String(); body != tt.expectedBody {
				t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, body)
			}
		})
	}
}

func TestFullMiddlewareChain(t *testing.T) {
	money.Store(1000)
	defer func() { money.Store(1000); bank.Store(0) }()

	payHandlerChain := RequestIDMiddleware(
		LoggingMiddleware(
			http.HandlerFunc(payHandler),
		),
	)

	server := httptest.NewServer(payHandlerChain)
	defer server.Close()

	client := &http.Client{}

	req1, _ := http.NewRequest("POST", server.URL+"/pay",
		bytes.NewBufferString("300"))
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp1.Body.Close()

	body1, _ := io.ReadAll(resp1.Body)
	if !strings.Contains(string(body1), "current balance: 700") {
		t.Errorf("Full chain payment failed. Response: %s", string(body1))
	}

	if resp1.Header.Get(RequestIDHeader) == "" {
		t.Error("Request ID header missing in response")
	}
}

func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		requestBody string
		setup       func()
		check       func(t *testing.T, moneyVal, bankVal int64)
	}{
		{
			name:        "Zero Amount Payment",
			handler:     payHandler,
			requestBody: "0",
			setup:       func() { money.Store(1000) },
			check: func(t *testing.T, moneyVal, bankVal int64) {
				if moneyVal != 1000 {
					t.Errorf("Zero amount should not change balance: got %d", moneyVal)
				}
			},
		},
		{
			name:        "Negative Amount",
			handler:     payHandler,
			requestBody: "-100",
			setup:       func() { money.Store(1000) },
			check: func(t *testing.T, moneyVal, bankVal int64) {
				if moneyVal != 1000 {
					t.Errorf("Negative amount should not change balance: got %d", moneyVal)
				}
			},
		},
		{
			name:        "Very Large Amount",
			handler:     payHandler,
			requestBody: "999999999999999999",
			setup:       func() { money.Store(1000) },
			check: func(t *testing.T, moneyVal, bankVal int64) {
				if moneyVal != 1000 {
					t.Errorf("Large amount should not change balance: got %d", moneyVal)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			defer func() { money.Store(1000); bank.Store(0) }()

			req := httptest.NewRequest("POST", "/test",
				bytes.NewBufferString(tt.requestBody))
			req = req.WithContext(context.WithValue(req.Context(),
				RequestIDContextKey, "edge-test"))

			w := httptest.NewRecorder()
			tt.handler(w, req)

			if tt.check != nil {
				tt.check(t, money.Load(), bank.Load())
			}
		})
	}
}

var logOutput = func(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}
