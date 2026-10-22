package main

import (
	"sync"
	"testing"
	"time"
)

func TestConsumeAttemptRejectsInvalidInputWithoutConsuming(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		state     string
	}{
		{name: "missing session", state: "expected-state"},
		{name: "missing state", sessionID: "session"},
		{name: "unknown session", sessionID: "unknown", state: "expected-state"},
		{name: "wrong state", sessionID: "session", state: "incorrect-data"},
		{name: "short state", sessionID: "session", state: "expected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := oauthAttempt{
				State:     "expected-state",
				Verifier:  "test-verifier",
				ExpiresAt: time.Now().Add(time.Minute),
			}
			server := &oauthServer{attempts: map[string]oauthAttempt{"session": want}}

			got, err := server.consumeAttempt(tt.sessionID, tt.state)
			if err == nil || got != (oauthAttempt{}) {
				t.Fatal("invalid input must return an error and no attempt")
			}
			got, err = server.consumeAttempt("session", want.State)
			if err != nil || got != want {
				t.Fatalf("valid callback after rejected input failed: %v", err)
			}
			got, err = server.consumeAttempt("session", want.State)
			if err == nil || got != (oauthAttempt{}) {
				t.Fatal("repeated callback must return an error and no attempt")
			}
		})
	}
}

func TestConsumeAttemptRejectsAndRemovesExpiredAttempt(t *testing.T) {
	server := &oauthServer{attempts: map[string]oauthAttempt{
		"expired": {
			State:     "expected-state",
			Verifier:  "test-verifier",
			ExpiresAt: time.Now().Add(-time.Minute),
		},
	}}

	got, err := server.consumeAttempt("expired", "expected-state")
	if err == nil || got != (oauthAttempt{}) {
		t.Fatal("expired attempt must return an error and no attempt")
	}
	if _, exists := server.attempts["expired"]; exists {
		t.Fatal("expired attempt must be removed")
	}
}

func TestConsumeAttemptAllowsOnlyOneConcurrentCallback(t *testing.T) {
	want := oauthAttempt{
		State:     "expected-state",
		Verifier:  "test-verifier",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	server := &oauthServer{attempts: map[string]oauthAttempt{"session": want}}

	const callers = 20
	type result struct {
		attempt oauthAttempt
		err     error
	}
	results := make(chan result, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			attempt, err := server.consumeAttempt("session", want.State)
			results <- result{attempt: attempt, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for result := range results {
		if result.err == nil {
			successes++
			if result.attempt != want {
				t.Error("successful callback returned a different attempt")
			}
		} else if result.attempt != (oauthAttempt{}) {
			t.Error("rejected callback returned an attempt")
		}
	}
	if successes != 1 {
		t.Fatalf("successful callbacks = %d, want 1", successes)
	}
}
