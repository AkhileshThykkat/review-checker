package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *OpenAICompat {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewOpenAICompat(srv.URL, "test-key", "test-model", nil)
	c.backoff = time.Millisecond
	return c
}

func okResponse(content string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, content)
}

func TestCompleteRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, okResponse("hello"))
	})

	got, _, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestCompleteReturnsUsage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"[]"}}],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`)
	})

	_, usage, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := Usage{PromptTokens: 120, CompletionTokens: 30, TotalTokens: 150}
	if usage != want {
		t.Errorf("usage = %+v, want %+v", usage, want)
	}
}

func TestCompleteRetriesOn5xx(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, okResponse("ok"))
	})

	if _, _, err := c.Complete(context.Background(), "sys", "user"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestCompleteGivesUpAfterMaxRetries(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, _, err := c.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("Complete succeeded, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want mention of 500", err)
	}
	if calls != maxRetries+1 {
		t.Errorf("calls = %d, want %d", calls, maxRetries+1)
	}
}

func TestCompleteDoesNotRetryAuthErrors(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	})

	if _, _, err := c.Complete(context.Background(), "sys", "user"); err == nil {
		t.Fatal("Complete succeeded, want error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retries)", calls)
	}
}

func TestCompleteRespectsContextDuringBackoff(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.backoff = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := c.Complete(ctx, "sys", "user")
		done <- err
	}()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Complete did not return after context cancel")
	}
}
