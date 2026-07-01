// Package llm defines the LLM client interface and an implementation for
// any OpenAI-compatible chat completions endpoint (DeepSeek, OpenRouter,
// OpenAI, Groq, Together, Ollama, Anthropic's compat layer, ...).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Client is the provider seam: a future non-OpenAI-shaped provider is a new
// implementation, not a rewrite.
type Client interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// OpenAICompat implements Client against POST {baseURL}/chat/completions.
type OpenAICompat struct {
	baseURL     string
	apiKey      string
	model       string
	temperature *float32
	http        *http.Client
	backoff     time.Duration // base delay, doubled per retry
}

// maxRetries is the number of re-attempts after the first request, on
// 429/5xx or transport errors. With a 1s base the waits are 1s, 2s, 4s.
const maxRetries = 3

// NewOpenAICompat builds a client. baseURL should include the version prefix
// if the provider uses one (e.g. https://api.deepseek.com/v1). A nil
// temperature is omitted from requests — required for models that reject
// the parameter (e.g. OpenAI o-series).
func NewOpenAICompat(baseURL, apiKey, model string, temperature *float32) *OpenAICompat {
	return &OpenAICompat{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		model:       model,
		temperature: temperature,
		http:        &http.Client{Timeout: 5 * time.Minute},
		backoff:     time.Second,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float32      `json:"temperature,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenAICompat) Complete(ctx context.Context, system, user string) (string, error) {
	payload, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: c.temperature,
	})
	if err != nil {
		return "", err
	}

	for attempt := 0; ; attempt++ {
		content, retryable, err := c.complete(ctx, payload)
		if err == nil {
			return content, nil
		}
		if !retryable || attempt == maxRetries {
			return "", err
		}
		delay := c.backoff << attempt
		log.Printf("llm request failed (attempt %d/%d), retrying in %s: %v",
			attempt+1, maxRetries+1, delay, err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
	}
}

// complete runs one request. retryable reports whether the failure is worth
// re-attempting: transport errors, 429, and 5xx. Other non-200s (401, 400)
// won't improve on retry.
func (c *OpenAICompat) complete(ctx context.Context, payload []byte) (content string, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", ctx.Err() == nil, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", true, fmt.Errorf("read llm response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return "", retryable, fmt.Errorf("llm returned %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, fmt.Errorf("decode llm response: %w", err)
	}
	if parsed.Error != nil {
		return "", false, fmt.Errorf("llm error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", false, fmt.Errorf("llm returned no choices")
	}
	return parsed.Choices[0].Message.Content, false, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
