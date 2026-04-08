package openclawruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"chatclaw/internal/services/providers"
)

// gatewayModelsCache maintains an in-memory cache of all models registered
// in the OpenClaw Gateway config, keyed by providerID -> modelIDSet.
// The cache is populated from openclaw.json on startup and refreshed after
// each successful config.sync or EnsureModelRegistered call.
// When dirty (e.g. after openclaw.json is modified externally), it triggers
// a full SyncConfig on the next SendOpenClawMessage.
type gatewayModelsCache struct {
	mu     sync.RWMutex
	models map[string]map[string]bool // providerID → modelIDSet
	dirty  bool                       // true when cache may be stale due to external changes
}

func newGatewayModelsCache() *gatewayModelsCache {
	return &gatewayModelsCache{
		models: make(map[string]map[string]bool),
		dirty:  true,
	}
}

// HasModel checks whether the given provider/model pair is in the cache.
// Returns false if the cache has not been loaded yet (dirty=true with no data).
func (c *gatewayModelsCache) HasModel(providerID, modelID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.dirty && len(c.models) == 0 {
		return false
	}
	modelSet, ok := c.models[providerID]
	if !ok {
		return false
	}
	return modelSet[modelID]
}

// MarkDirty marks the cache as potentially stale, so the next send will
// trigger a full SyncConfig before checking again.
func (c *gatewayModelsCache) MarkDirty() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dirty = true
}

// IsDirty reports whether the cache has been loaded and is considered clean.
func (c *gatewayModelsCache) IsDirty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dirty
}

// LoadFromOpenClawJSON reads openclaw.json and populates the cache with all
// models listed under config.models.providers. Returns an error if the file
// cannot be read or parsed; the cache remains unchanged on error.
func (c *gatewayModelsCache) LoadFromOpenClawJSON(configPath string) error {
	models, err := parseOpenClawJSONModels(configPath)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.models = models
	c.dirty = false
	c.mu.Unlock()
	return nil
}

// RefreshFromGateway fetches config.get from the Gateway and updates the cache
// with the models returned. Returns an error if the Gateway is not ready or
// the request fails.
func (c *gatewayModelsCache) RefreshFromGateway(ctx context.Context, manager *Manager) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var live map[string]any
	if err := manager.Request(ctx, "config.get", map[string]any{}, &live); err != nil {
		return fmt.Errorf("config.get: %w", err)
	}

	providers := extractModelsProvidersFromGatewayGet(live)
	models := make(map[string]map[string]bool)
	for provID, pv := range providers {
		pm, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		arr, ok := pm["models"].([]any)
		if !ok {
			models[provID] = make(map[string]bool)
			continue
		}
		modelSet := make(map[string]bool)
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok {
				if id, ok := m["id"].(string); ok && id != "" {
					modelSet[id] = true
				}
			}
		}
		models[provID] = modelSet
	}

	c.mu.Lock()
	c.models = models
	c.dirty = false
	c.mu.Unlock()
	return nil
}

// AddModel adds a single provider/model pair to the cache.
// Called after EnsureModelRegistered succeeds to keep the cache up-to-date.
func (c *gatewayModelsCache) AddModel(providerID, modelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.models[providerID] == nil {
		c.models[providerID] = make(map[string]bool)
	}
	c.models[providerID][modelID] = true
}

// parseOpenClawJSONModels reads the given openclaw.json file and returns
// a map of providerID → modelIDSet extracted from the models section.
func parseOpenClawJSONModels(configPath string) (map[string]map[string]bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read openclaw.json: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse openclaw.json: %w", err)
	}

	// Try config.models.providers first (bundled runtime format).
	models, ok := extractModelsProvidersRaw(raw, "config", "models", "providers")
	if !ok {
		// Fallback: top-level models.providers (user runtime format).
		models, ok = extractModelsProvidersRaw(raw, "models", "providers")
		if !ok {
			return make(map[string]map[string]bool), nil
		}
	}

	result := make(map[string]map[string]bool)
	for provID, pv := range models {
		pm, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		arr, ok := pm["models"].([]any)
		if !ok {
			result[provID] = make(map[string]bool)
			continue
		}
		modelSet := make(map[string]bool)
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok {
				if id, ok := m["id"].(string); ok && id != "" {
					modelSet[id] = true
				}
			}
		}
		result[provID] = modelSet
	}
	return result, nil
}

// extractModelsProvidersRaw navigates nested maps along keys and returns
// the map found at that path, or (nil, false) if the path does not exist.
func extractModelsProvidersRaw(root map[string]any, keys ...string) (map[string]any, bool) {
	current := any(root)
	for _, k := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := m[k]
		if !exists {
			return nil, false
		}
		current = next
	}
	result, ok := current.(map[string]any)
	return result, ok
}

// SectionBuilder produces a partial config map for one top-level section.
// Example return: {"models": {"mode": "replace", "providers": {...}}}
type SectionBuilder func(ctx context.Context) (map[string]any, error)

type sectionEntry struct {
	name    string
	builder SectionBuilder
}

// ConfigService manages all config sections and provides a unified
// config.get + merge + config.patch flow to the OpenClaw Gateway.
type ConfigService struct {
	manager      *Manager
	providersSvc ProvidersSvcProvider // set via SetProvidersService

	mu           sync.Mutex
	sections     []sectionEntry
	lastPatchRaw string
	cache        *gatewayModelsCache // local cache of models in openclaw.json / gateway config
}

// SetModelsCache injects the gateway models cache so that Sync and
// EnsureModelRegistered can update it after successfully pushing config.
func (s *ConfigService) SetModelsCache(cache *gatewayModelsCache) {
	s.cache = cache
}

// ProvidersSvcProvider abstracts the subset of *providers.ProvidersService needed
// by ConfigService, avoiding a direct import that would cause a circular dependency.
type ProvidersSvcProvider interface {
	GetProviderWithModels(providerID string) (*providers.ProviderWithModels, error)
	ListProviders() ([]providers.Provider, error)
}

func NewConfigService(manager *Manager) *ConfigService {
	return &ConfigService{manager: manager}
}

// SetProvidersService injects the ProvidersService for per-model registration.
func (s *ConfigService) SetProvidersService(svc ProvidersSvcProvider) {
	s.providersSvc = svc
}

// ResponsesEndpointSection enables gateway HTTP OpenResponses via config.patch only.
// Do not use `openclaw config set` before gateway start: that writes openclaw.json once,
// then the process applies --auth/--token and persists again, then ChatClaw sends config.patch —
// multiple competing writes make the file watcher see gateway.auth/tailscale/meta churn and
// hybrid reload restarts the gateway in a loop.
func ResponsesEndpointSection() SectionBuilder {
	return func(ctx context.Context) (map[string]any, error) {
		_ = ctx
		return map[string]any{
			"gateway": map[string]any{
				"http": map[string]any{
					"endpoints": map[string]any{
						"responses": map[string]any{"enabled": true},
					},
				},
			},
		}, nil
	}
}

// Register adds a named section builder. The name is for logging only;
// the builder's returned map keys determine the actual config sections.
func (s *ConfigService) Register(name string, builder SectionBuilder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sections = append(s.sections, sectionEntry{name: name, builder: builder})
}

// Sync collects all sections, merges them into one patch, and pushes to Gateway.
// Calls are serialised via mutex to avoid baseHash races. If the merged patch
// is identical to the last successful push, the call is a no-op.
func (s *ConfigService) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.manager.IsReady() {
		return fmt.Errorf("gateway not ready")
	}

	merged := make(map[string]any)
	for _, sec := range s.sections {
		partial, err := sec.builder(ctx)
		if err != nil {
			s.log("openclaw: section builder %q failed: %v", sec.name, err)
			continue
		}
		for k, v := range partial {
			merged[k] = v
		}
	}

	raw, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("marshal config patch: %w", err)
	}
	rawStr := string(raw)

	if rawStr == s.lastPatchRaw {
		return nil
	}

	syncCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	preview := rawStr
	if len(preview) > 512 {
		preview = preview[:512] + "...(truncated)"
	}
	s.log("openclaw: Sync pushing config len=%d preview=%s", len(rawStr), preview)

	var getResult struct {
		Hash string `json:"hash"`
	}
	if err := s.manager.Request(syncCtx, "config.get", map[string]any{}, &getResult); err != nil {
		return fmt.Errorf("config.get: %w", err)
	}

	if err := s.manager.Request(syncCtx, "config.patch", map[string]any{
		"raw":      rawStr,
		"baseHash": getResult.Hash,
	}, nil); err != nil {
		// Patch failed; do NOT update lastPatchRaw so next call can retry.
		return fmt.Errorf("config.patch: %w", err)
	}

	s.lastPatchRaw = rawStr
	s.log("openclaw: config sync completed")

	// Refresh the in-memory cache to match what the Gateway now has,
	// so subsequent SendOpenClawMessage calls can skip full SyncConfig.
	if s.cache != nil {
		_ = s.cache.RefreshFromGateway(ctx, s.manager)
	}
	return nil
}

func (s *ConfigService) log(format string, args ...any) {
	if s.manager != nil && s.manager.app != nil {
		s.manager.app.Logger.Info(fmt.Sprintf(format, args...))
	}
}

// EnsureModelRegistered checks whether the given provider/model exists under
// config.models.providers on the Gateway (supports both top-level models and nested config.models).
// If missing, it pushes the same models section as full sync: {"models":{"mode":"replace","providers":{...}}}
// so the payload matches OpenClaw's schema (a bare {"models":{"chatwiki":...}} is ignored by the gateway).
func (s *ConfigService) EnsureModelRegistered(ctx context.Context, providerID, modelID string) error {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return nil
	}

	if !s.manager.IsReady() {
		return fmt.Errorf("gateway not ready")
	}

	ps, ok := s.providersSvc.(*providers.ProvidersService)
	if !ok || ps == nil {
		return fmt.Errorf("providers service not available for model sync")
	}

	// Force-refresh ChatWiki catalog so GetProviderWithModels sees latest data.
	// Without this, the in-memory cache may hold stale data that won't be in DB.
	ClearChatWikiSyncCache()

	syncCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Verify model exists in ChatClaw catalog (with refreshed cache).
	pwm, err := ps.GetProviderWithModels(providerID)
	if err != nil {
		return fmt.Errorf("get provider with models: %w", err)
	}
	modelFound := false
	for _, group := range pwm.ModelGroups {
		for _, m := range group.Models {
			if m.ModelID == modelID && m.Enabled {
				modelFound = true
				break
			}
		}
		if modelFound {
			break
		}
	}
	if !modelFound {
		return fmt.Errorf("model %s/%s not in ChatClaw catalog or disabled", providerID, modelID)
	}

	var live map[string]any
	if err := s.manager.Request(syncCtx, "config.get", map[string]any{}, &live); err != nil {
		return fmt.Errorf("config.get: %w", err)
	}

	hash := configGetHashFromResponse(live)
	liveProvs := extractModelsProvidersFromGatewayGet(live)
	if gatewayProviderHasModel(liveProvs, providerID, modelID) {
		s.log("openclaw: model already on gateway, skip models patch, provider=%s model=%s", providerID, modelID)
		return nil
	}

	s.log("openclaw: model missing on gateway, pushing full models section, provider=%s model=%s", providerID, modelID)

	// Build with fresh ChatWiki data (cache was cleared above).
	modelsSection, err := BuildModelsSectionPatch(ps)
	if err != nil {
		return fmt.Errorf("build models section: %w", err)
	}

	raw, err := json.Marshal(map[string]any{"models": modelsSection})
	if err != nil {
		return fmt.Errorf("marshal models patch: %w", err)
	}

	preview := string(raw)
	if len(preview) > 512 {
		preview = preview[:512] + "...(truncated)"
	}
	s.log("openclaw: models patch JSON len=%d preview=%s", len(raw), preview)

	if strings.TrimSpace(hash) == "" {
		return fmt.Errorf("config.get returned empty hash")
	}
	if err := s.manager.Request(syncCtx, "config.patch", map[string]any{
		"raw":      string(raw),
		"baseHash": hash,
	}, nil); err != nil {
		return fmt.Errorf("config.patch models: %w", err)
	}

	s.log("openclaw: models section patched for gateway, provider=%s model=%s", providerID, modelID)

	// Keep the cache in sync so the next SendOpenClawMessage sees this model.
	if s.cache != nil {
		s.cache.AddModel(providerID, modelID)
	}
	return nil
}

func configGetHashFromResponse(root map[string]any) string {
	if root == nil {
		return ""
	}
	if h, ok := root["hash"].(string); ok && strings.TrimSpace(h) != "" {
		return h
	}
	if inner, ok := root["result"].(map[string]any); ok {
		if h, ok := inner["hash"].(string); ok && strings.TrimSpace(h) != "" {
			return h
		}
	}
	if inner, ok := root["data"].(map[string]any); ok {
		if h, ok := inner["hash"].(string); ok && strings.TrimSpace(h) != "" {
			return h
		}
	}
	return ""
}

func extractModelsProvidersFromGatewayGet(root map[string]any) map[string]any {
	if root == nil {
		return nil
	}
	if inner, ok := root["result"].(map[string]any); ok {
		if p := extractModelsProvidersFromGatewayGet(inner); p != nil {
			return p
		}
	}
	if inner, ok := root["data"].(map[string]any); ok {
		if p := extractModelsProvidersFromGatewayGet(inner); p != nil {
			return p
		}
	}
	if cfg, ok := root["config"].(map[string]any); ok {
		if models, ok := cfg["models"].(map[string]any); ok {
			if p, ok := models["providers"].(map[string]any); ok {
				return p
			}
		}
	}
	if models, ok := root["models"].(map[string]any); ok {
		if p, ok := models["providers"].(map[string]any); ok {
			return p
		}
	}
	return nil
}

func gatewayProviderHasModel(providers map[string]any, providerID, modelID string) bool {
	if providers == nil {
		return false
	}
	pv, ok := providers[providerID]
	if !ok {
		return false
	}
	pm, ok := pv.(map[string]any)
	if !ok {
		return false
	}
	arr, ok := pm["models"].([]any)
	if !ok {
		return false
	}
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == modelID {
			return true
		}
	}
	return false
}
