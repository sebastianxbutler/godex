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

	"godex/pkg/admin"
	"godex/pkg/auth"
	"godex/pkg/client"
	"godex/pkg/config"
	"godex/pkg/harness"
	"godex/pkg/metrics"
	"godex/pkg/payments"
	"godex/pkg/protocol"
	"godex/pkg/router"
	"godex/pkg/sse"
)

var errNoFlusher = errors.New("response writer does not support flushing")

// ModelEntry defines a supported model with optional base URL override.
type ModelEntry struct {
	ID      string
	BaseURL string
}

// Config controls proxy behavior.
type Config struct {
	Listen          string
	APIKey          string
	Model           string
	Models          []ModelEntry
	BaseURL         string
	AllowRefresh    bool
	AllowAnyKey     bool
	AuthPath        string
	Originator      string
	UserAgent       string
	CacheTTL        time.Duration
	LogLevel        string
	LogRequests     bool
	KeysPath        string
	RateLimit       string
	Burst           int
	QuotaTokens     int64
	StatsPath       string
	StatsSummary    string
	StatsMaxBytes   int64
	StatsMaxBackups int
	EventsPath      string
	EventsMaxBytes  int64
	EventsBackups   int
	AuditPath       string
	AuditMaxBytes   int64
	AuditBackups    int
	MeterWindow     time.Duration
	AdminSocket     string
	Payments        payments.Config
	Backends        BackendsConfig
	Metrics         MetricsConfig
	HarnessRouter   *router.Router
}

// BackendsConfig configures available LLM backends.
type BackendsConfig struct {
	Codex     CodexBackendConfig
	Anthropic AnthropicBackendConfig
	Custom    map[string]config.CustomBackendConfig
	Routing   RoutingConfig
}

// MetricsConfig configures per-backend metrics collection.
type MetricsConfig struct {
	Enabled     bool
	Path        string
	LogRequests bool
}

// CodexBackendConfig configures the Codex/ChatGPT backend.
type CodexBackendConfig struct {
	Enabled         bool
	BaseURL         string
	CredentialsPath string
}

// AnthropicBackendConfig configures the Anthropic backend.
type AnthropicBackendConfig struct {
	Enabled          bool
	CredentialsPath  string
	DefaultMaxTokens int
}

// RoutingConfig configures model-to-backend routing.
type RoutingConfig struct {
	Default  string
	Patterns map[string][]string
	Aliases  map[string]string
}

type Server struct {
	cfg        Config
	cache      *Cache
	httpClient *http.Client
	authStore  *auth.Store
	logger     *Logger
	audit      *AuditLogger
	keys       *KeyStore
	limiters   *LimiterStore
	metrics    *metrics.Collector
	usage      *UsageStore
	payments   payments.Gateway
	models     map[string]ModelEntry
	harnessRouter *router.Router
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
	// api-key optional when using key store; --allow-any-key bypasses auth entirely
	if strings.TrimSpace(cfg.KeysPath) == "" {
		cfg.KeysPath = DefaultKeysPath()
	}
	if strings.TrimSpace(cfg.StatsPath) == "" {
		cfg.StatsPath = ""
	}
	if strings.TrimSpace(cfg.StatsSummary) == "" {
		cfg.StatsSummary = DefaultStatsSummaryPath()
	}
	if cfg.StatsMaxBytes == 0 {
		cfg.StatsMaxBytes = 10 * 1024 * 1024
	}
	if cfg.StatsMaxBackups == 0 {
		cfg.StatsMaxBackups = 3
	}
	if strings.TrimSpace(cfg.EventsPath) == "" {
		cfg.EventsPath = DefaultEventsPath()
	}
	if cfg.EventsMaxBytes == 0 {
		cfg.EventsMaxBytes = 1024 * 1024
	}
	if cfg.EventsBackups == 0 {
		cfg.EventsBackups = 3
	}
	if strings.TrimSpace(cfg.RateLimit) == "" {
		cfg.RateLimit = "60/m"
	}
	if cfg.Burst == 0 {
		cfg.Burst = 10
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

	var keys *KeyStore
	if !cfg.AllowAnyKey {
		keysPath := strings.TrimSpace(cfg.KeysPath)
		if keysPath == "" {
			keysPath = DefaultKeysPath()
		}
		keys, err = LoadKeyStore(keysPath)
		if err != nil {
			return err
		}
	}

	usage := NewUsageStore(cfg.StatsPath, cfg.StatsSummary, cfg.StatsMaxBytes, cfg.StatsMaxBackups, cfg.MeterWindow, cfg.EventsPath, cfg.EventsMaxBytes, cfg.EventsBackups)
	_ = usage.LoadFromFile()
	limiters := NewLimiterStore(cfg.RateLimit, cfg.Burst)
	payGateway := payments.NewTokenMeterGateway(cfg.Payments)

	// Build models map
	models := make(map[string]ModelEntry)
	if len(cfg.Models) > 0 {
		for _, m := range cfg.Models {
			baseURL := m.BaseURL
			if baseURL == "" {
				baseURL = cfg.BaseURL
			}
			models[m.ID] = ModelEntry{ID: m.ID, BaseURL: baseURL}
		}
	} else if cfg.Model != "" {
		models[cfg.Model] = ModelEntry{ID: cfg.Model, BaseURL: cfg.BaseURL}
	}

	// Initialize metrics collector
	metricsCollector, err := metrics.NewCollector(metrics.Config{
		Enabled:     cfg.Metrics.Enabled,
		Path:        cfg.Metrics.Path,
		LogRequests: cfg.Metrics.LogRequests,
	})
	if err != nil {
		return fmt.Errorf("init metrics: %w", err)
	}

	s := &Server{
		cfg:            cfg,
		cache:          NewCache(cfg.CacheTTL),
		httpClient:     http.DefaultClient,
		authStore:      store,
		logger:         NewLogger(ParseLogLevel(cfg.LogLevel)),
		audit:          NewAuditLogger(cfg.AuditPath, cfg.AuditMaxBytes, cfg.AuditBackups),
		keys:           keys,
		limiters:       limiters,
		usage:          usage,
		payments:       payGateway,
		models:        models,
		harnessRouter: cfg.HarnessRouter,
		metrics:       metricsCollector,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models/", s.handleModelByID) // must come before /v1/models
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/pricing", s.handlePricing)
	mux.HandleFunc("/v1/responses", s.handleResponses)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/health", s.handleHealth)

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if strings.TrimSpace(cfg.AdminSocket) != "" {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			adminSrv := admin.New(cfg.AdminSocket, adminAdapter{keys: keys})
			_ = adminSrv.Start(ctx)
		}()
	}


	return server.ListenAndServe()
}

func (s *Server) clientForSession(sessionID string) *client.Client {
	return s.clientForSessionWithBaseURL(sessionID, s.cfg.BaseURL)
}

func (s *Server) clientForSessionWithBaseURL(sessionID string, baseURL string) *client.Client {
	if baseURL == "" {
		baseURL = s.cfg.BaseURL
	}
	return client.New(s.httpClient, s.authStore, client.Config{
		BaseURL:      baseURL,
		Originator:   s.cfg.Originator,
		UserAgent:    s.cfg.UserAgent,
		SessionID:    sessionID,
		AllowRefresh: s.cfg.AllowRefresh,
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if ok, _ := s.allowRequest(w, r, key); !ok {
		return
	}

	// Try to get models from harness router first, then backend router
	var data []OpenAIModel
	if s.harnessRouter != nil {
		models := s.harnessRouter.AllModels(r.Context())
		for _, m := range models {
			data = append(data, OpenAIModel{
				ID:      m.ID,
				Object:  "model",
				OwnedBy: "godex",
			})
		}
	}
	// Fall back to configured models
	if len(data) == 0 {
		for _, m := range s.models {
			data = append(data, OpenAIModel{
				ID:      m.ID,
				Object:  "model",
				OwnedBy: "godex",
			})
		}
	}

	// Final fallback to default model
	if len(data) == 0 {
		data = []OpenAIModel{{
			ID:      s.cfg.Model,
			Object:  "model",
			OwnedBy: "godex",
		}}
	}

	resp := OpenAIModelsResponse{
		Object: "list",
		Data:   data,
	}
	writeJSON(w, http.StatusOK, resp)
	s.logRequest(r, http.StatusOK, start)
}

// handleModelByID handles GET /v1/models/{model_id}
func (s *Server) handleModelByID(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if ok, _ := s.allowRequest(w, r, key); !ok {
		return
	}

	// Extract model ID from path
	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if modelID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("model ID required"))
		s.logRequest(r, http.StatusBadRequest, start)
		return
	}

	// Expand alias
	expandedID := modelID
	if s.harnessRouter != nil {
		expandedID = s.harnessRouter.ExpandAlias(modelID)
	}

	// Check if harness router can handle this model
	if s.harnessRouter != nil {
		if h := s.harnessRouter.HarnessFor(expandedID); h != nil {
			resp := OpenAIModelDetail{
				ID:      expandedID,
				Object:  "model",
				OwnedBy: "godex",
				Backend: h.Name(),
			}
			if modelID != expandedID {
				resp.Alias = modelID
			}
			writeJSON(w, http.StatusOK, resp)
			s.logRequest(r, http.StatusOK, start)
			return
		}
	}

	// Model not found
	writeError(w, http.StatusNotFound, fmt.Errorf("model %q not found", modelID))
	s.logRequest(r, http.StatusNotFound, start)
}

// OpenAIModelDetail is the response for GET /v1/models/{id}
type OpenAIModelDetail struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	OwnedBy     string `json:"owned_by"`
	DisplayName string `json:"display_name,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Alias       string `json:"alias,omitempty"`
}

func (s *Server) resolveModel(model string) (ModelEntry, bool) {
	if model == "" {
		model = s.cfg.Model
	}
	// Expand alias
	if s.harnessRouter != nil {
		model = s.harnessRouter.ExpandAlias(model)
	}
	if m, ok := s.models[model]; ok {
		return m, true
	}
	// If harness router has a harness for this model, allow it
	if s.harnessRouter != nil && s.harnessRouter.HarnessFor(model) != nil {
		return ModelEntry{ID: model, BaseURL: ""}, true
	}
	// fallback to default if no models configured
	if len(s.models) == 0 {
		return ModelEntry{ID: model, BaseURL: s.cfg.BaseURL}, true
	}
	return ModelEntry{}, false
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req OpenAIResponsesRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		s.logRequest(r, http.StatusBadRequest, start)
		return
	}
	modelEntry, ok := s.resolveModel(req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("model %q not available", req.Model))
		s.logRequest(r, http.StatusBadRequest, start)
		return
	}
	req.Model = modelEntry.ID
	key, ok := s.requireAuthOrPayment(w, r, req.Model)
	if !ok {
		return
	}
	if ok, reason := s.allowRequest(w, r, key); !ok {
		if reason == "tokens" {
			_ = s.issuePaymentChallenge(w, r, "topup", key.ID, req.Model)
		}
		return
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

	// Try harness-based routing first
	if h := s.harnessForModel(req.Model); h != nil {
		turn := buildTurnFromResponses(req.Model, instructions, input, tools, nil)
		var auditReqJSON json.RawMessage
		if s.audit != nil {
			auditReqJSON, _ = json.Marshal(req)
		}

		if !stream {
			s.harnessResponsesNonStream(requestContext(r), w, h, turn, req.Model, key, start, auditReqJSON, sessionKey)
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
		if err := s.harnessResponsesStream(requestContext(r), w, flusher, h, turn, req.Model, key, start, auditReqJSON, sessionKey); err != nil {
			writeError(w, http.StatusBadGateway, err)
			s.logRequest(r, http.StatusBadGateway, start)
			return
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
		s.logRequest(r, http.StatusOK, start)
		return
	}

	// Legacy backend path
	cl := s.clientForSessionWithBaseURL(sessionKey, modelEntry.BaseURL)
	if !stream {
		result, err := cl.StreamAndCollect(requestContext(r), codexReq)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			s.logRequest(r, http.StatusBadGateway, start)
			return
		}
		s.cache.SaveToolCalls(sessionKey, toolCallsFromResult(result))
		resp := responsesResponseFromResult(req.Model, result)
		writeJSON(w, http.StatusOK, resp)
		s.recordUsage(r, key, http.StatusOK, nil)
		s.logRequest(r, http.StatusOK, start)

		// Audit log (non-streaming)
		if s.audit != nil {
			var toolNames []string
			for _, tc := range result.ToolCalls {
				toolNames = append(toolNames, tc.Name)
			}
			entry := AuditEntry{
				KeyID:         key.ID,
				KeyLabel:      key.Label,
				Method:        r.Method,
				Path:          r.URL.Path,
				Model:         req.Model,
				Status:        http.StatusOK,
				ElapsedMs:     time.Since(start).Milliseconds(),
				ToolCount:     len(req.Tools),
				HasToolCalls:  len(result.ToolCalls) > 0,
				ToolCallNames: toolNames,
				OutputText:    result.Text,
			}
			if result.Usage != nil {
				entry.TokensIn = result.Usage.InputTokens
				entry.TokensOut = result.Usage.OutputTokens
			}
			if reqJSON, err := json.Marshal(req); err == nil {
				entry.Request = reqJSON
			}
			s.audit.Log(entry)
		}
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

	var usage *protocol.Usage
	err = cl.StreamResponses(requestContext(r), codexReq, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if ev.Value.Response != nil && ev.Value.Response.Usage != nil {
			usage = ev.Value.Response.Usage
		}
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
	s.recordUsage(r, key, http.StatusOK, usage)
	s.logRequest(r, http.StatusOK, start)

	// Audit log
	if s.audit != nil {
		var toolNames []string
		for _, name := range callNames {
			toolNames = append(toolNames, name)
		}
		inputCount := 0
		if len(req.Input) > 0 {
			var items []json.RawMessage
			if json.Unmarshal(req.Input, &items) == nil {
				inputCount = len(items)
			}
		}
		entry := AuditEntry{
			KeyID:         key.ID,
			KeyLabel:      key.Label,
			Method:        r.Method,
			Path:          r.URL.Path,
			Model:         req.Model,
			Status:        http.StatusOK,
			ElapsedMs:     time.Since(start).Milliseconds(),
			InputItems:    inputCount,
			ToolCount:     len(req.Tools),
			HasToolCalls:  len(callNames) > 0,
			ToolCallNames: toolNames,
			OutputText:    collector.OutputText(),
		}
		if usage != nil {
			entry.TokensIn = usage.InputTokens
			entry.TokensOut = usage.OutputTokens
		}
		if reqJSON, err := json.Marshal(req); err == nil {
			entry.Request = reqJSON
		}
		s.audit.Log(entry)
	}
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (*KeyRecord, bool) {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		if s.cfg.AllowAnyKey {
			return &KeyRecord{ID: "anonymous", Label: "anonymous"}, true
		}
		writeError(w, http.StatusUnauthorized, errors.New("missing bearer token"))
		return nil, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if s.cfg.AllowAnyKey {
		return &KeyRecord{ID: hashToken(token), Label: "anonymous"}, true
	}
	// static api_key disabled; use key store or --allow-any-key
	if s.keys == nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid bearer token"))
		return nil, false
	}
	rec, ok := s.keys.Validate(token)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("invalid bearer token"))
		return nil, false
	}
	return &rec, true
}

// requestContext returns the request context, enriched with a provider key
// if the X-Provider-Key header is present.
func requestContext(r *http.Request) context.Context {
	ctx := r.Context()
	if key := strings.TrimSpace(r.Header.Get("X-Provider-Key")); key != "" {
		ctx = harness.WithProviderKey(ctx, key)
	}
	return ctx
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

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		s.logRequest(r, http.StatusMethodNotAllowed, start)
		return
	}

	stats := s.metrics.Stats()
	
	// Build response with backend stats
	response := map[string]any{
		"backends": stats,
	}
	
	writeJSON(w, http.StatusOK, response)
	s.logRequest(r, http.StatusOK, start)
}

func (s *Server) logRequest(r *http.Request, status int, start time.Time) {
	if !s.cfg.LogRequests || s.logger == nil {
		return
	}
	elapsed := time.Since(start)
	s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", fmt.Sprintf("%d", status), "elapsed", elapsed.String())
}

// recordMetric records a request metric for a backend.
func (s *Server) recordMetric(backend, model string, start time.Time, status, errMsg string, usage *protocol.Usage) {
	if s.metrics == nil {
		return
	}
	m := metrics.RequestMetric{
		Timestamp: start,
		Backend:   backend,
		Model:     model,
		Latency:   time.Since(start),
		Status:    status,
		Error:     errMsg,
	}
	if usage != nil {
		m.TokensIn = usage.InputTokens
		m.TokensOut = usage.OutputTokens
	}
	s.metrics.Record(m)
}
