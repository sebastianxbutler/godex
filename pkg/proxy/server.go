package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"godex/pkg/auth"
	"godex/pkg/client"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

var errNoFlusher = errors.New("response writer does not support flushing")

// Config controls proxy behavior.
type Config struct {
	Listen       string
	APIKey       string
	Model        string
	BaseURL      string
	AllowRefresh bool
	AllowAnyKey  bool
	AuthPath     string
	Originator   string
	UserAgent    string
	CacheTTL     time.Duration
	LogLevel     string
	LogRequests  bool
}

type Server struct {
	cfg        Config
	cache      *Cache
	httpClient *http.Client
	authStore  *auth.Store
	logger     *Logger
}

func Run(cfg Config) error {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:39001"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.2-codex"
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 6 * time.Hour
	}
	if cfg.APIKey == "" && !cfg.AllowAnyKey {
		return fmt.Errorf("api key is required")
	}

	authPath := strings.TrimSpace(cfg.AuthPath)
	var err error
	if authPath == "" {
		authPath, err = auth.DefaultPath()
		if err != nil {
			return err
		}
	}
	store, err := auth.Load(authPath)
	if err != nil {
		return err
	}

	s := &Server{
		cfg:        cfg,
		cache:      NewCache(cfg.CacheTTL),
		httpClient: http.DefaultClient,
		authStore:  store,
		logger:     NewLogger(ParseLogLevel(cfg.LogLevel)),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/responses", s.handleResponses)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/health", s.handleHealth)

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return server.ListenAndServe()
}

func (s *Server) clientForSession(sessionID string) *client.Client {
	return client.New(s.httpClient, s.authStore, client.Config{
		BaseURL:      s.cfg.BaseURL,
		Originator:   s.cfg.Originator,
		UserAgent:    s.cfg.UserAgent,
		SessionID:    sessionID,
		AllowRefresh: s.cfg.AllowRefresh,
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !s.requireAuth(w, r) {
		return
	}
	resp := OpenAIModelsResponse{
		Object: "list",
		Data: []OpenAIModel{{
			ID:      s.cfg.Model,
			Object:  "model",
			OwnedBy: "godex",
		}},
	}
	writeJSON(w, http.StatusOK, resp)
	s.logRequest(r, http.StatusOK, start)
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !s.requireAuth(w, r) {
		return
	}
	var req OpenAIResponsesRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		s.logRequest(r, http.StatusBadRequest, start)
		return
	}
	if req.Model == "" {
		req.Model = s.cfg.Model
	}

	sessionKey := s.sessionKey(req.User, r)
	items, err := parseOpenAIInput(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		s.logRequest(r, http.StatusBadRequest, start)
		return
	}
	input, system, err := buildSystemAndInput(sessionKey, items, s.cache)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		s.logRequest(r, http.StatusBadRequest, start)
		return
	}
	instructions := mergeInstructions(req.Instructions, system)
	instructions = s.resolveInstructions(sessionKey, instructions)

	tools := mapTools(req.Tools)
	toolChoice, tools := resolveToolChoice(req.ToolChoice, tools)

	stream := false
	if req.Stream != nil {
		stream = *req.Stream
	}

	codexReq := protocol.ResponsesRequest{
		Model:             req.Model,
		Instructions:      instructions,
		Input:             input,
		Tools:             tools,
		ToolChoice:        toolChoice,
		ParallelToolCalls: boolPtrValue(req.ParallelToolCalls),
		Store:             false,
		Stream:            true,
		Include:           []string{},
		PromptCacheKey:    sessionKey,
	}

	cl := s.clientForSession(sessionKey)
	if !stream {
		result, err := cl.StreamAndCollect(r.Context(), codexReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			s.logRequest(r, http.StatusBadGateway, start)
			return
		}
		s.cache.SaveToolCalls(sessionKey, toolCallsFromResult(result))
		resp := responsesResponseFromResult(req.Model, result)
		writeJSON(w, http.StatusOK, resp)
		s.logRequest(r, http.StatusOK, start)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errNoFlusher)
		s.logRequest(r, http.StatusInternalServerError, start)
		return
	}

	collector := sse.NewCollector()
	callNames := map[string]string{}

	err = cl.StreamResponses(r.Context(), codexReq, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if ev.Value.Type == "response.output_item.added" && ev.Value.Item != nil {
			if ev.Value.Item.Type == "function_call" && ev.Value.Item.CallID != "" {
				callNames[ev.Value.Item.CallID] = ev.Value.Item.Name
			}
		}
		if err := writeSSE(w, flusher, ev.Raw); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		s.logRequest(r, http.StatusBadGateway, start)
		return
	}

	calls := map[string]ToolCall{}
	for callID, name := range callNames {
		calls[callID] = ToolCall{Name: name, Arguments: collector.FunctionArgs(callID)}
	}
	s.cache.SaveToolCalls(sessionKey, calls)
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
	s.logRequest(r, http.StatusOK, start)
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.APIKey == "" {
		if s.cfg.AllowAnyKey {
			return true
		}
		writeError(w, http.StatusUnauthorized, errors.New("missing API key"))
		return false
	}
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		writeError(w, http.StatusUnauthorized, errors.New("missing bearer token"))
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if !s.cfg.AllowAnyKey && token != s.cfg.APIKey {
		writeError(w, http.StatusUnauthorized, errors.New("invalid bearer token"))
		return false
	}
	return true
}

func (s *Server) sessionKey(user string, r *http.Request) string {
	if strings.TrimSpace(user) != "" {
		return strings.TrimSpace(user)
	}
	if val := strings.TrimSpace(r.Header.Get("x-openclaw-session-key")); val != "" {
		return val
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return "anonymous"
}

func (s *Server) resolveInstructions(sessionKey, instructions string) string {
	if strings.TrimSpace(instructions) == "" {
		if cached, ok := s.cache.GetInstructions(sessionKey); ok {
			return cached
		}
		return defaultInstructions()
	}
	s.cache.SaveInstructions(sessionKey, instructions)
	return instructions
}

func responsesResponseFromResult(model string, result client.StreamResult) OpenAIResponsesResponse {
	resp := OpenAIResponsesResponse{
		ID:     newResponseID("resp"),
		Object: "response",
		Model:  model,
		Output: []OpenAIRespItem{},
	}
	if strings.TrimSpace(result.Text) != "" {
		resp.Output = append(resp.Output, OpenAIRespItem{
			Type: "message",
			Role: "assistant",
			Content: []OpenAIRespContent{{
				Type: "output_text",
				Text: result.Text,
			}},
		})
	}
	for _, call := range result.ToolCalls {
		resp.Output = append(resp.Output, OpenAIRespItem{
			Type:      "function_call",
			Name:      call.Name,
			CallID:    call.CallID,
			Arguments: call.Arguments,
		})
	}
	return resp
}

func toolCallsFromResult(result client.StreamResult) map[string]ToolCall {
	calls := map[string]ToolCall{}
	for _, call := range result.ToolCalls {
		calls[call.CallID] = ToolCall{Name: call.Name, Arguments: call.Arguments}
	}
	return calls
}

func readJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 20*1024*1024))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("empty body")
	}
	return json.Unmarshal(body, out)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	if err == nil {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    "proxy_error",
		},
	})
}

func writeSSE(w io.Writer, flusher http.Flusher, payload any) error {
	var data []byte
	switch v := payload.(type) {
	case json.RawMessage:
		data = v
	default:
		buf, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		data = buf
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func newResponseID(prefix string) string {
	if prefix == "" {
		prefix = "resp"
	}
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func boolPtrValue(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

func defaultInstructions() string {
	return "You are a helpful assistant."
}

func (s *Server) ServeWithContext(ctx context.Context) error {
	server := &http.Server{Addr: s.cfg.Listen}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	return server.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	s.logRequest(r, http.StatusOK, start)
}

func (s *Server) logRequest(r *http.Request, status int, start time.Time) {
	if !s.cfg.LogRequests || s.logger == nil {
		return
	}
	elapsed := time.Since(start)
	s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", fmt.Sprintf("%d", status), "elapsed", elapsed.String())
}
