// localrunnerctl is a small terminal client for LocalRunner's localhost API.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultAddress = "http://127.0.0.1:8080"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("localrunnerctl", flag.ContinueOnError)
	flags.SetOutput(stderr)
	address := flags.String("address", envOrDefault("LOCALRUNNER_API_URL", defaultAddress), "LocalRunner API URL")
	flags.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if err := validateAddress(*address); err != nil {
		return err
	}
	remaining := flags.Args()
	if len(remaining) == 0 {
		flags.Usage()
		return errors.New("a command is required")
	}

	client := &apiClient{baseURL: strings.TrimRight(*address, "/"), http: &http.Client{Timeout: 2 * time.Minute}}
	command, args := remaining[0], remaining[1:]
	switch command {
	case "status":
		return client.printJSON(context.Background(), stdout, http.MethodGet, "/v1/runners/status", nil)
	case "metrics":
		return client.printJSON(context.Background(), stdout, http.MethodGet, "/metrics", nil)
	case "profiles":
		return client.printJSON(context.Background(), stdout, http.MethodGet, "/v1/models/profiles", nil)
	case "register":
		if len(args) != 1 {
			return errors.New("usage: localrunnerctl register <model.gguf>")
		}
		return client.printJSON(context.Background(), stdout, http.MethodPost, "/v1/models/register", map[string]string{"path": args[0]})
	case "activate":
		if len(args) != 1 {
			return errors.New("usage: localrunnerctl activate <profile>")
		}
		return client.printJSON(context.Background(), stdout, http.MethodPost, "/v1/models/activate", map[string]string{"name": args[0]})
	case "start":
		if len(args) != 0 {
			return errors.New("usage: localrunnerctl start")
		}
		return client.printJSON(context.Background(), stdout, http.MethodPost, "/v1/runners/start", nil)
	case "stop":
		if len(args) != 0 {
			return errors.New("usage: localrunnerctl stop")
		}
		return client.printJSON(context.Background(), stdout, http.MethodPost, "/v1/runners/stop", nil)
	case "chat":
		return runChat(client, args, stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runChat(client *apiClient, arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	flags.SetOutput(stderr)
	stream := flags.Bool("stream", false, "print server-sent events as they arrive")
	maxTokens := flags.Int("max-tokens", 256, "maximum generated tokens (1-512)")
	temperature := flags.Float64("temperature", 0, "sampling temperature (0-2)")
	thinking := flags.Bool("thinking", false, "enable Qwen thinking mode")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	prompt := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if prompt == "" {
		return errors.New("usage: localrunnerctl chat [flags] <prompt>")
	}
	payload := map[string]any{
		"messages":             []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens":           *maxTokens,
		"temperature":          *temperature,
		"stream":               *stream,
		"chat_template_kwargs": map[string]bool{"enable_thinking": *thinking},
	}
	if *stream {
		return client.stream(context.Background(), stdout, payload)
	}
	return client.chat(context.Background(), stdout, payload)
}

type apiClient struct {
	baseURL string
	http    *http.Client
}

func (client *apiClient) printJSON(ctx context.Context, stdout io.Writer, method, path string, payload any) error {
	response, err := client.request(ctx, method, path, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkResponse(response); err != nil {
		return err
	}
	return prettyPrint(stdout, response.Body)
}

func (client *apiClient) chat(ctx context.Context, stdout io.Writer, payload any) error {
	response, err := client.request(ctx, http.MethodPost, "/v1/chat/completions", payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkResponse(response); err != nil {
		return err
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(response.Body).Decode(&completion); err != nil {
		return fmt.Errorf("decode completion: %w", err)
	}
	if len(completion.Choices) == 0 {
		return errors.New("completion contained no choices")
	}
	_, err = fmt.Fprintln(stdout, completion.Choices[0].Message.Content)
	return err
}

func (client *apiClient) stream(ctx context.Context, stdout io.Writer, payload any) error {
	response, err := client.request(ctx, http.MethodPost, "/v1/chat/completions", payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkResponse(response); err != nil {
		return err
	}
	_, err = io.Copy(stdout, response.Body)
	return err
}

func (client *apiClient) request(ctx context.Context, method, path string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return client.http.Do(request)
}

func checkResponse(response *http.Response) error {
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return fmt.Errorf("API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
}

func prettyPrint(stdout io.Writer, source io.Reader) error {
	var value any
	if err := json.NewDecoder(source).Decode(&value); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(encoded))
	return err
}

func validateAddress(address string) error {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return errors.New("--address must be an http://host:port URL")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

const usage = `Usage: localrunnerctl [--address URL] <command> [arguments]

Commands:
  status                         Show runner state and health.
  profiles                       List configured model profiles.
  register <model.gguf>          Register an explicit local model path.
  activate <profile>             Select a configured profile.
  start | stop                   Start or stop the managed runner.
  metrics                        Show process-lifetime metrics.
  chat [flags] <prompt>          Send a chat completion.

Chat flags:
  --stream                       Print raw SSE events as they arrive.
  --max-tokens N                 Limit output to 1-512 tokens (default 256).
  --temperature N                Sampling temperature from 0 to 2 (default 0).
  --thinking                     Enable Qwen thinking mode.
`
