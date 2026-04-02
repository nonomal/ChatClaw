package openclawchannels

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"chatclaw/internal/define"
	"chatclaw/internal/errs"
	"chatclaw/internal/services/channels"
)

const (
	openClawWhatsappPluginPackage = "@openclaw/whatsapp@latest"
	openClawWhatsappChannelID     = "whatsapp"
	whatsappLoginWaitTimeout      = 8 * time.Minute
	whatsappQRReadTimeout         = 3 * time.Minute
	whatsappPluginInstallTimeout  = 5 * time.Minute
)

func (s *OpenClawChannelService) isWhatsappPluginInstalledLocally() bool {
	cfg, _, err := loadOpenClawJSONConfig()
	if err != nil {
		return false
	}
	plugins, _ := cfg["plugins"].(map[string]any)
	if plugins == nil {
		return false
	}

	installs, _ := plugins["installs"].(map[string]any)
	if installs != nil {
		if raw, ok := installs[openClawWhatsappChannelID].(map[string]any); ok {
			if installPath := strings.TrimSpace(fmt.Sprint(raw["installPath"])); installPath != "" {
				if info, statErr := os.Stat(installPath); statErr == nil && info.IsDir() {
					return true
				}
			}
		}
	}

	load, _ := plugins["load"].(map[string]any)
	paths, _ := load["paths"].([]any)
	for _, raw := range paths {
		p := strings.TrimSpace(fmt.Sprint(raw))
		if p == "" || !strings.Contains(strings.ToLower(p), "/whatsapp") {
			continue
		}
		if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (s *OpenClawChannelService) isWhatsappPluginEnabled() bool {
	cfg, _, err := loadOpenClawJSONConfig()
	if err != nil {
		return false
	}
	plugins, _ := cfg["plugins"].(map[string]any)
	if plugins == nil {
		return false
	}
	entries, _ := plugins["entries"].(map[string]any)
	if entries == nil {
		return false
	}
	entry, _ := entries[openClawWhatsappChannelID].(map[string]any)
	if entry == nil {
		return false
	}
	enabled, _ := entry["enabled"].(bool)
	return enabled
}

func (s *OpenClawChannelService) isWhatsappPluginReadyForUse() bool {
	return s.isWhatsappPluginInstalledLocally() && s.isWhatsappPluginEnabled()
}

type whatsappLoginSession struct {
	accountID string
}

// WhatsappChannelPreparation mirrors WeChat: plugin install state before QR flow.
type WhatsappChannelPreparation struct {
	Ready          bool `json:"ready"`
	Installing     bool `json:"installing"`
	StartedInstall bool `json:"started_install"`
}

// WhatsappQRCodeResult is returned by GenerateWhatsappQRCode for the Wails UI.
type WhatsappQRCodeResult struct {
	QrcodeDataURL string `json:"qrcode_data_url"`
	SessionKey    string `json:"session_key"`
}

// WhatsappLoginResult is returned by WaitForWhatsappLogin.
type WhatsappLoginResult struct {
	Connected bool   `json:"connected"`
	AccountID string `json:"account_id"`
	Message   string `json:"message"`
	ChannelID int64  `json:"channel_id"`
}

func (s *OpenClawChannelService) ensureWhatsappLoginMap() {
	if s.whatsappLogins == nil {
		s.whatsappLogins = make(map[string]*whatsappLoginSession)
	}
}

func (s *OpenClawChannelService) cancelAllWhatsappLoginSessions() {
	s.whatsappLoginMu.Lock()
	defer s.whatsappLoginMu.Unlock()
	s.ensureWhatsappLoginMap()
	for k := range s.whatsappLogins {
		delete(s.whatsappLogins, k)
	}
}

func containsWhatsappPluginMarker(out string) bool {
	o := strings.ToLower(strings.TrimSpace(out))
	if o == "" {
		return false
	}
	// Bundled channel shows as stock:whatsapp in `plugins list` Source column.
	if strings.Contains(o, "stock:whatsapp") {
		return true
	}
	// npm / user-installed path segments.
	if strings.Contains(o, "@openclaw/whatsapp") || strings.Contains(o, "openclaw/whatsapp") {
		return true
	}
	// CLI table wraps the Name column (no contiguous "@openclaw/whatsapp"). ID column is "whatsapp".
	if strings.Contains(o, "│ whatsapp │") || strings.Contains(o, "| whatsapp |") {
		return true
	}
	return false
}

func (s *OpenClawChannelService) ensureOpenClawWhatsappPluginInstalled(ctx context.Context) error {
	installed, err := s.isOpenClawWhatsappPluginInstalled(ctx)
	if err == nil && installed {
		return nil
	}

	if _, installErr := s.execOpenClawPluginCLI(ctx, "plugins", "install", openClawWhatsappPluginPackage); installErr != nil {
		installMsg := strings.ToLower(installErr.Error())
		already := strings.Contains(installMsg, "plugin already exists") || strings.Contains(installMsg, "already installed") || containsWhatsappPluginMarker(installMsg)
		if !already {
			return fmt.Errorf("openclaw plugins install %s: %w", openClawWhatsappPluginPackage, installErr)
		}
	}

	verifyInstalled, verifyErr := s.isOpenClawWhatsappPluginInstalled(ctx)
	if verifyErr != nil {
		return fmt.Errorf("verify whatsapp plugin: %w", verifyErr)
	}
	if !verifyInstalled {
		return fmt.Errorf("plugin %s not found after installation", openClawWhatsappPluginPackage)
	}
	return nil
}

func isWhatsappInstallTransientFailure(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "rate limit") || strings.Contains(m, "429") ||
		strings.Contains(m, "timeout") || strings.Contains(m, "deadline") ||
		strings.Contains(m, "econnrefused") || strings.Contains(m, "enotfound") ||
		strings.Contains(m, "etimedout") || strings.Contains(m, "network") ||
		strings.Contains(m, "temporary failure") || strings.Contains(m, "connection reset") ||
		strings.Contains(m, "context canceled")
}

func (s *OpenClawChannelService) isOpenClawWhatsappPluginInstalled(ctx context.Context) (bool, error) {
	out, err := s.execOpenClawPluginCLI(ctx, "plugins", "list")
	if err != nil {
		return false, err
	}
	return containsWhatsappPluginMarker(string(out)), nil
}

func (s *OpenClawChannelService) ensureWhatsappPluginBackgroundInstallStarted() bool {
	s.whatsappPluginInstallMu.Lock()
	defer s.whatsappPluginInstallMu.Unlock()
	if s.whatsappPluginInstallRunning {
		return false
	}
	s.whatsappPluginInstallRunning = true
	go s.runWhatsappPluginInstallWithRetry()
	return true
}

func (s *OpenClawChannelService) runWhatsappPluginInstallWithRetry() {
	defer func() {
		s.whatsappPluginInstallMu.Lock()
		s.whatsappPluginInstallRunning = false
		s.whatsappPluginInstallMu.Unlock()
	}()

	const maxAttempts = 4
	baseDelay := 3 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), whatsappPluginInstallTimeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				s.app.Logger.Warn("openclaw: whatsapp plugin background install aborted", "error", ctx.Err())
				return
			case <-time.After(delay):
			}
		}
		if err := s.ensureOpenClawReady(); err != nil {
			lastErr = err
			s.app.Logger.Debug("openclaw: whatsapp plugin install waiting for openclaw", "error", err)
			continue
		}
		if err := s.ensureOpenClawWhatsappPluginInstalled(ctx); err != nil {
			lastErr = err
			if isWhatsappInstallTransientFailure(err.Error()) {
				s.app.Logger.Warn("openclaw: whatsapp plugin full install retry", "attempt", attempt+1, "error", err)
				continue
			}
			s.app.Logger.Warn("openclaw: whatsapp plugin background install failed", "error", err)
			return
		}
		s.app.Logger.Info("openclaw: whatsapp plugin background install completed")
		return
	}
	s.app.Logger.Warn("openclaw: whatsapp plugin background install exhausted retries", "error", lastErr)
}

// PrepareWhatsappChannel ensures the official WhatsApp OpenClaw plugin is installed (sync) when missing.
func (s *OpenClawChannelService) PrepareWhatsappChannel() (*WhatsappChannelPreparation, error) {
	if err := s.ensureOpenClawReady(); err != nil {
		return nil, err
	}
	if s.isWhatsappPluginReadyForUse() {
		return &WhatsappChannelPreparation{Ready: true}, nil
	}
	started := s.ensureWhatsappPluginBackgroundInstallStarted()
	return &WhatsappChannelPreparation{
		Ready:          false,
		Installing:     true,
		StartedInstall: started,
	}, nil
}

func whatsappLoginOutputSuggestsMissingPluginOrChannel(blob string) bool {
	b := strings.ToLower(blob)
	if b == "" {
		return false
	}
	if strings.Contains(b, "whatsapp") && strings.Contains(b, "unknown channel") {
		return true
	}
	if strings.Contains(b, "provider unavailable") || strings.Contains(b, "provider unsupported") {
		return true
	}
	if strings.Contains(b, "plugin") {
		if strings.Contains(b, "not found") || strings.Contains(b, "not installed") ||
			strings.Contains(b, "missing") || strings.Contains(b, "eplugin") {
			return true
		}
	}
	return false
}

type whatsappGatewayLoginStartResult struct {
	QRDataURL string `json:"qrDataUrl"`
	Message   string `json:"message"`
}

type whatsappGatewayLoginWaitResult struct {
	Connected bool   `json:"connected"`
	Message   string `json:"message"`
}

func buildWhatsappWebLoginParams(accountID string, timeout time.Duration) map[string]any {
	params := map[string]any{
		"timeoutMs": int(timeout / time.Millisecond),
	}
	if id := strings.TrimSpace(accountID); id != "" {
		params["accountId"] = id
	}
	return params
}

// GenerateWhatsappQRCode requests a QR data URL from the OpenClaw gateway's
// web.login.start flow instead of scraping the terminal QR output from the CLI.
func (s *OpenClawChannelService) GenerateWhatsappQRCode() (*WhatsappQRCodeResult, error) {
	if err := s.ensureOpenClawReady(); err != nil {
		return nil, err
	}
	if !s.isWhatsappPluginReadyForUse() {
		ctx, cancel := context.WithTimeout(context.Background(), whatsappPluginInstallTimeout)
		defer cancel()
		if err := s.ensureOpenClawWhatsappPluginInstalled(ctx); err != nil {
			s.ensureWhatsappPluginBackgroundInstallStarted()
			return nil, errs.Wrap("error.whatsapp_plugin_not_ready", err)
		}
	}

	s.cancelAllWhatsappLoginSessions()

	accountID := strings.TrimSpace(s.readFirstWhatsappAccountIDFromOpenClawJSON())
	reqCtx, reqCancel := context.WithTimeout(context.Background(), whatsappQRReadTimeout)
	defer reqCancel()

	var start whatsappGatewayLoginStartResult
	if err := s.openclawManager.Request(reqCtx, "web.login.start",
		buildWhatsappWebLoginParams(accountID, whatsappQRReadTimeout), &start); err != nil {
		if whatsappLoginOutputSuggestsMissingPluginOrChannel(err.Error()) {
			s.ensureWhatsappPluginBackgroundInstallStarted()
			return nil, errs.Wrap("error.whatsapp_plugin_not_ready", err)
		}
		return nil, errs.Wrap("error.whatsapp_qr_not_found", err)
	}
	if strings.TrimSpace(start.QRDataURL) == "" {
		if msg := strings.TrimSpace(start.Message); msg != "" {
			if whatsappLoginOutputSuggestsMissingPluginOrChannel(msg) {
				s.ensureWhatsappPluginBackgroundInstallStarted()
				return nil, errs.Wrap("error.whatsapp_plugin_not_ready", fmt.Errorf("%s", msg))
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, errs.New("error.whatsapp_qr_not_found")
	}

	sessionKey := uuid.NewString()
	s.whatsappLoginMu.Lock()
	s.ensureWhatsappLoginMap()
	s.whatsappLogins[sessionKey] = &whatsappLoginSession{accountID: accountID}
	s.whatsappLoginMu.Unlock()

	return &WhatsappQRCodeResult{
		QrcodeDataURL: strings.TrimSpace(start.QRDataURL),
		SessionKey:    sessionKey,
	}, nil
}

// CancelWhatsappLogin clears the local session mapping for the pending QR flow.
func (s *OpenClawChannelService) CancelWhatsappLogin(sessionKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		s.cancelAllWhatsappLoginSessions()
		return
	}
	s.whatsappLoginMu.Lock()
	s.ensureWhatsappLoginMap()
	delete(s.whatsappLogins, sessionKey)
	s.whatsappLoginMu.Unlock()
}

// WaitForWhatsappLogin waits on the OpenClaw gateway's QR-login session, then creates a local channel row.
func (s *OpenClawChannelService) WaitForWhatsappLogin(sessionKey string, channelName string) (*WhatsappLoginResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, errs.New("error.whatsapp_login_failed")
	}
	s.whatsappLoginMu.Lock()
	s.ensureWhatsappLoginMap()
	sess, ok := s.whatsappLogins[sessionKey]
	s.whatsappLoginMu.Unlock()
	if !ok || sess == nil {
		return nil, errs.New("error.whatsapp_login_failed")
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), whatsappLoginWaitTimeout)
	defer waitCancel()

	var waitResp whatsappGatewayLoginWaitResult
	waitErr := s.openclawManager.Request(waitCtx, "web.login.wait",
		buildWhatsappWebLoginParams(sess.accountID, whatsappLoginWaitTimeout), &waitResp)

	s.whatsappLoginMu.Lock()
	delete(s.whatsappLogins, sessionKey)
	s.whatsappLoginMu.Unlock()

	if waitErr != nil {
		if waitCtx.Err() == context.DeadlineExceeded {
			return &WhatsappLoginResult{Connected: false, Message: "timeout"}, errs.New("error.whatsapp_login_timeout")
		}
		return &WhatsappLoginResult{
			Connected: false,
			Message:   waitErr.Error(),
		}, nil
	}
	if !waitResp.Connected {
		return &WhatsappLoginResult{
			Connected: false,
			Message:   strings.TrimSpace(waitResp.Message),
		}, nil
	}

	accountID := strings.TrimSpace(sess.accountID)
	if accountID == "" {
		accountID = s.readFirstWhatsappAccountIDFromOpenClawJSON()
	}
	name := strings.TrimSpace(channelName)
	if name == "" {
		name = "WhatsApp"
	}
	extraConfig, extraErr := json.Marshal(appCredentialsJSON{
		Platform:  channels.PlatformWhatsapp,
		AccountID: accountID,
	})
	if extraErr != nil {
		extraConfig = []byte(fmt.Sprintf(`{"platform":"whatsapp","account_id":%q}`, accountID))
	}

	ch, chErr := s.upsertWhatsappChannelRecord(accountID, name, string(extraConfig))
	if chErr != nil {
		s.app.Logger.Warn("openclaw: failed to upsert whatsapp channel after login", "error", chErr)
		return &WhatsappLoginResult{Connected: false, Message: chErr.Error()}, nil
	}
	s.app.Logger.Info("openclaw: whatsapp channel record ready", "channel_id", ch.ID, "accountId", accountID)

	var agentID int64
	targetOpenClawAgentID := strings.TrimSpace(s.readWhatsappConfiguredOpenClawAgentID(accountID))
	if targetOpenClawAgentID == "" {
		targetOpenClawAgentID = define.OpenClawMainAgentID
	}
	targetLocalAgent, ensureAgentErr := s.agentsSvc.EnsureAgentRecordByOpenClawAgentID(targetOpenClawAgentID, "")
	if ensureAgentErr != nil {
		s.app.Logger.Warn("openclaw: failed to ensure whatsapp target agent before bind", "channel_id", ch.ID, "openclaw_agent_id", targetOpenClawAgentID, "error", ensureAgentErr)
	} else if targetLocalAgent != nil {
		if bindErr := s.channelSvc.BindAgent(ch.ID, targetLocalAgent.ID); bindErr != nil {
			s.app.Logger.Warn("openclaw: failed to bind whatsapp channel to configured agent", "channel_id", ch.ID, "agent_id", targetLocalAgent.ID, "openclaw_agent_id", targetOpenClawAgentID, "error", bindErr)
		} else {
			agentID = targetLocalAgent.ID
			s.app.Logger.Info("openclaw: whatsapp channel bound to configured assistant", "channel_id", ch.ID, "agent_id", agentID, "openclaw_agent_id", targetOpenClawAgentID)
		}
	}

	if agentID > 0 {
		if rErr := s.syncChannelRoutingBinding(ch.ID, agentID); rErr != nil {
			s.app.Logger.Warn("openclaw: failed to apply whatsapp openclaw routing after login", "channel_id", ch.ID, "error", rErr)
		}
	} else {
		s.app.Logger.Warn("openclaw: whatsapp channel left unbound after login", "channel_id", ch.ID, "accountId", accountID)
	}

	if err := s.setChannelOnlineStatus(context.Background(), ch.ID, true); err != nil {
		s.app.Logger.Warn("openclaw: failed to set whatsapp channel online", "channel_id", ch.ID, "error", err)
	}

	return &WhatsappLoginResult{
		Connected: true,
		AccountID: accountID,
		ChannelID: ch.ID,
	}, nil
}

func (s *OpenClawChannelService) readFirstWhatsappAccountIDFromOpenClawJSON() string {
	cfg, _, err := loadOpenClawJSONConfig()
	if err != nil {
		return "default"
	}
	chans, _ := cfg["channels"].(map[string]any)
	if chans == nil {
		return "default"
	}
	wa, _ := chans["whatsapp"].(map[string]any)
	if wa == nil {
		return "default"
	}
	accts, _ := wa["accounts"].(map[string]any)
	if accts == nil || len(accts) == 0 {
		return "default"
	}
	if _, ok := accts["default"]; ok {
		return "default"
	}
	for k := range accts {
		if id := strings.TrimSpace(k); id != "" {
			return id
		}
	}
	return "default"
}

func (s *OpenClawChannelService) readWhatsappConfiguredOpenClawAgentID(accountID string) string {
	cfg, _, err := loadOpenClawJSONConfig()
	if err != nil {
		return ""
	}

	accountID = strings.TrimSpace(accountID)
	chans, _ := cfg["channels"].(map[string]any)
	if chans != nil {
		if wa, _ := chans["whatsapp"].(map[string]any); wa != nil {
			if accts, _ := wa["accounts"].(map[string]any); accts != nil {
				if acct, _ := accts[accountID].(map[string]any); acct != nil {
					if agentID, _ := acct["agentId"].(string); strings.TrimSpace(agentID) != "" {
						return strings.TrimSpace(agentID)
					}
				}
			}
		}
	}

	bindings, _ := cfg["bindings"].([]any)
	for _, raw := range bindings {
		binding, _ := raw.(map[string]any)
		if binding == nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(binding["type"])) != "route" {
			continue
		}
		match, _ := binding["match"].(map[string]any)
		if match == nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(match["channel"])) != openClawWhatsappChannelID {
			continue
		}
		if accountID != "" && strings.TrimSpace(fmt.Sprint(match["accountId"])) != accountID {
			continue
		}
		if agentID := strings.TrimSpace(fmt.Sprint(binding["agentId"])); agentID != "" {
			return agentID
		}
	}

	return ""
}

func (s *OpenClawChannelService) findWhatsappChannelByAccountID(accountID string) (*channelModel, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, nil
	}

	db, err := s.db()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var models []channelModel
	if err := db.NewSelect().
		Model(&models).
		Where("ch.platform = ?", channels.PlatformWhatsapp).
		Where("ch.openclaw_scope = ?", true).
		OrderExpr("ch.id DESC").
		Scan(ctx); err != nil {
		return nil, errs.Wrap("error.channel_read_failed", err)
	}

	for i := range models {
		if extractWhatsappAccountID(models[i].ExtraConfig) == accountID {
			return &models[i], nil
		}
	}
	return nil, nil
}

func (s *OpenClawChannelService) upsertWhatsappChannelRecord(accountID, name, extraConfig string) (*channels.Channel, error) {
	existing, err := s.findWhatsappChannelByAccountID(accountID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return s.channelSvc.CreateChannel(channels.CreateChannelInput{
			Platform:       channels.PlatformWhatsapp,
			Name:           name,
			Avatar:         "",
			ConnectionType: channels.ConnTypeGateway,
			ExtraConfig:    extraConfig,
			OpenClawScope:  true,
		})
	}

	var input channels.UpdateChannelInput
	var changed bool
	if strings.TrimSpace(existing.Name) != strings.TrimSpace(name) {
		name = strings.TrimSpace(name)
		input.Name = &name
		changed = true
	}
	if strings.TrimSpace(existing.ExtraConfig) != strings.TrimSpace(extraConfig) {
		extraConfig = strings.TrimSpace(extraConfig)
		input.ExtraConfig = &extraConfig
		changed = true
	}
	if !changed {
		dto := existing.toDTO()
		return &dto, nil
	}
	return s.channelSvc.UpdateChannel(existing.ID, input)
}

func (s *OpenClawChannelService) runOpenClawWhatsappLogout(ctx context.Context, accountID string) error {
	args := []string{"channels", "logout", "--channel", openClawWhatsappChannelID}
	if id := strings.TrimSpace(accountID); id != "" && !strings.EqualFold(id, "default") {
		args = append(args, "--account", id)
	}
	_, err := s.execOpenClawCLIWithRetry(ctx, args...)
	return err
}

func (s *OpenClawChannelService) purgeWhatsappChannelOpenClawIntegration(m *channelModel) error {
	accountID := openClawManagedAccountID(channels.PlatformWhatsapp, m.ID, m.ExtraConfig)
	ctx, cancel := context.WithTimeout(context.Background(), openClawChannelSyncTimeout)
	defer cancel()
	if err := s.runOpenClawWhatsappLogout(ctx, accountID); err != nil {
		s.app.Logger.Warn("openclaw: whatsapp logout during purge failed", "error", err)
	}
	if err := s.removeManagedRouteBinding(openClawWhatsappChannelID, accountID); err != nil {
		return err
	}
	return s.restartOpenClawGateway()
}

func (s *OpenClawChannelService) connectWhatsappViaPlugin(id int64, m *channelModel) error {
	if m.AgentID == 0 {
		return errs.New("error.channel_connect_requires_agent")
	}
	pluginCtx, pluginCancel := context.WithTimeout(context.Background(), whatsappPluginInstallTimeout)
	defer pluginCancel()
	if err := s.ensureOpenClawWhatsappPluginInstalled(pluginCtx); err != nil {
		return errs.Wrap("error.whatsapp_plugin_not_ready", err)
	}
	syncCtx, syncCancel := context.WithTimeout(context.Background(), openClawChannelSyncTimeout)
	defer syncCancel()
	if err := s.syncChannelRoutingBinding(id, m.AgentID); err != nil {
		return wrapOpenClawSyncErr(err, "error.channel_connect_failed", map[string]any{"Name": m.Name})
	}
	return s.setChannelOnlineStatus(syncCtx, id, true)
}

func (s *OpenClawChannelService) disconnectWhatsappViaPlugin(id int64, m *channelModel) error {
	ctx, cancel := context.WithTimeout(context.Background(), openClawChannelSyncTimeout)
	defer cancel()
	routeAccount := openClawManagedAccountID(channels.PlatformWhatsapp, id, m.ExtraConfig)
	if err := s.removeManagedRouteBinding(openClawWhatsappChannelID, routeAccount); err != nil {
		s.app.Logger.Warn("openclaw: whatsapp route remove on disconnect failed", "error", err)
	}
	if err := s.restartOpenClawGateway(); err != nil {
		return errs.Wrap("error.channel_disconnect_failed", err)
	}
	return s.setChannelOnlineStatus(ctx, id, false)
}
