package openclawchannels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"chatclaw/internal/errs"
	"chatclaw/internal/services/channels"
)

const (
	wechatCLIPackage           = "@tencent-weixin/openclaw-weixin-cli"
	wechatCLIPackageWithTag    = "@tencent-weixin/openclaw-weixin-cli@latest"
	wechatPluginID             = "openclaw-weixin"
	wechatPluginInstallTimeout = 5 * time.Minute
	wechatLoginWaitTimeout     = 5 * time.Minute
	wechatExtensionSubdir      = "extensions/openclaw-weixin"
)

// isWechatPluginInstalledLocally checks whether the WeChat OpenClaw plugin directory exists.
func (s *OpenClawChannelService) isWechatPluginInstalledLocally() bool {
	stateDir, err := s.openclawManager.BundleStateDir()
	if err != nil {
		return false
	}
	pluginDir := filepath.Join(stateDir, wechatExtensionSubdir)
	info, err := os.Stat(pluginDir)
	return err == nil && info.IsDir()
}

// IsWechatPluginInstalled reports whether the WeChat OpenClaw plugin is installed (Wails UI).
func (s *OpenClawChannelService) IsWechatPluginInstalled() bool {
	return s.isWechatPluginInstalledLocally()
}

// isWechatChannelConfigStale reads openclaw.json and returns whether a
// channels.openclaw-weixin entry exists in a stale (legacy) format — i.e. it
// was written by old code that did not include an "accounts" key. The gateway
// needs a proper entry with "accounts" to recognise the plugin as a configured
// channel. An entry without "accounts" may have been written before this
// convention was established and should be replaced.
//
// An entry that already has "accounts" is considered valid and is NOT stale.
func (s *OpenClawChannelService) isWechatChannelConfigStale() bool {
	stateDir, err := s.openclawManager.BundleStateDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "openclaw.json"))
	if err != nil {
		return false
	}
	var cfg map[string]any
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	chans, _ := cfg["channels"].(map[string]any)
	if chans == nil {
		return false
	}
	entry, exists := chans[wechatPluginID]
	if !exists {
		return false
	}
	// Valid entry: has an "accounts" key (even if the map is empty).
	entryMap, _ := entry.(map[string]any)
	if entryMap != nil {
		if _, hasAccounts := entryMap["accounts"]; hasAccounts {
			return false // not stale — valid format
		}
	}
	return true // stale: entry exists but lacks the "accounts" key
}

// upsertWechatChannelConfig writes (or updates) the channels.openclaw-weixin
// section in openclaw.json. The plugin (v2.1.1+) requires this entry to be
// present so the gateway recognises the channel at startup and calls
// startAccount for each account discovered from the credential file index.
//
// Only metadata (name, enabled) is stored in the config; the actual
// credentials (token, baseUrl) live in the plugin's file storage.
func (s *OpenClawChannelService) upsertWechatChannelConfig(accountID, name string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("upsertWechatChannelConfig: accountID is empty")
	}

	cfg, configPath, err := loadOpenClawJSONConfig()
	if err != nil {
		return err
	}

	channels, _ := cfg["channels"].(map[string]any)
	if channels == nil {
		channels = map[string]any{}
	}

	weixinSection, _ := channels[wechatPluginID].(map[string]any)
	if weixinSection == nil {
		weixinSection = map[string]any{}
	}

	accounts, _ := weixinSection["accounts"].(map[string]any)
	if accounts == nil {
		accounts = map[string]any{}
	}

	accountEntry := map[string]any{
		"enabled": true,
	}
	if n := strings.TrimSpace(name); n != "" {
		accountEntry["name"] = n
	}
	accounts[accountID] = accountEntry

	weixinSection["accounts"] = accounts
	channels[wechatPluginID] = weixinSection
	cfg["channels"] = channels

	return saveOpenClawJSONConfig(configPath, cfg)
}

// removeStaleWechatChannelConfigEntry removes the channels.openclaw-weixin entry from
// openclaw.json only when it is in stale (legacy) format — i.e. no "accounts" key.
// Returns true if the file was modified.
func (s *OpenClawChannelService) removeStaleWechatChannelConfigEntry() (bool, error) {
	stateDir, err := s.openclawManager.BundleStateDir()
	if err != nil {
		return false, err
	}
	configPath := filepath.Join(stateDir, "openclaw.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false, err
	}
	chans, _ := cfg["channels"].(map[string]any)
	if chans == nil {
		return false, nil
	}
	if _, exists := chans[wechatPluginID]; !exists {
		return false, nil
	}
	delete(chans, wechatPluginID)
	if len(chans) == 0 {
		delete(cfg, "channels")
	}
	updated, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(configPath, append(updated, '\n'), 0o644)
}


// isWechatPluginEnabled reads the openclaw.json config file and returns whether the
// wechat plugin's enabled flag is set to true.
func (s *OpenClawChannelService) isWechatPluginEnabled() bool {
	stateDir, err := s.openclawManager.BundleStateDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "openclaw.json"))
	if err != nil {
		return false
	}
	var cfg map[string]any
	if json.Unmarshal(data, &cfg) != nil {
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
	entry, _ := entries[wechatPluginID].(map[string]any)
	if entry == nil {
		return false
	}
	enabled, _ := entry["enabled"].(bool)
	return enabled
}

// IsWechatPluginEnabled reports whether the WeChat OpenClaw plugin is enabled (Wails UI).
func (s *OpenClawChannelService) IsWechatPluginEnabled() bool {
	return s.isWechatPluginEnabled()
}

// ensureWechatPluginInstalled checks the WeChat plugin's installed and enabled states,
// installs or enables it as needed, and restarts the gateway only when the state changed.
func (s *OpenClawChannelService) ensureWechatPluginInstalled(ctx context.Context) error {
	needsRestart := false

	// Step 1: install if the plugin directory is absent.
	if !s.isWechatPluginInstalledLocally() {
		s.app.Logger.Info("openclaw: wechat plugin not found, installing", "package", wechatCLIPackage)
		out, err := s.openclawManager.ExecNpx(ctx, "-y", wechatCLIPackageWithTag, "install")
		if err != nil {
			outStr := strings.ToLower(string(out))
			if strings.Contains(outStr, "already installed") || strings.Contains(outStr, "plugin already exists") {
				s.app.Logger.Info("openclaw: wechat plugin already installed (marker in output)")
			} else {
				return fmt.Errorf("install wechat plugin: %w", err)
			}
		} else {
			s.app.Logger.Info("openclaw: wechat plugin installed successfully")
		}
		needsRestart = true
	} else {
		s.app.Logger.Info("openclaw: wechat plugin already installed, skipping install")
	}

	// Step 2: enable if the plugin is not yet enabled in openclaw.json.
	if !s.isWechatPluginEnabled() {
		s.app.Logger.Info("openclaw: wechat plugin not enabled, enabling")
		if _, err := s.execOpenClawCLIWithRetry(ctx, "config", "set",
			"plugins.entries.openclaw-weixin.enabled", "true", "--strict-json"); err != nil {
			s.app.Logger.Warn("openclaw: failed to enable wechat plugin in config", "error", err)
		} else {
			s.app.Logger.Info("openclaw: wechat plugin enabled successfully")
			needsRestart = true
		}
	} else {
		s.app.Logger.Info("openclaw: wechat plugin already enabled, skipping enable")
	}

	// Step 3: remove any stale (legacy) channels.openclaw-weixin config entry.
	// A stale entry is one written by old code that lacked the "accounts" key;
	// the gateway would try to activate such an entry and could produce
	// unexpected behaviour. Valid entries (those that include "accounts") are
	// left untouched — the plugin requires them to recognise the channel at
	// startup (see upsertWechatChannelConfig).
	if s.isWechatChannelConfigStale() {
		s.app.Logger.Info("openclaw: removing stale channels.openclaw-weixin config entry (missing accounts key)")
		if changed, err := s.removeStaleWechatChannelConfigEntry(); err != nil {
			s.app.Logger.Warn("openclaw: failed to remove stale channel config entry", "error", err)
		} else if changed {
			s.app.Logger.Info("openclaw: removed stale channels.openclaw-weixin config entry")
			needsRestart = true
		}
	} else {
		s.app.Logger.Info("openclaw: channels.openclaw-weixin config entry is absent or already valid, skipping stale removal")
	}

	// Step 4: restart gateway only if the install or enable state changed.
	if needsRestart {
		if err := s.restartOpenClawGateway(); err != nil {
			s.app.Logger.Warn("openclaw: gateway restart after wechat plugin setup failed", "error", err)
		} else {
			// Give the gateway a moment to come back online before making method calls.
			time.Sleep(3 * time.Second)
		}
	}

	return nil
}

// EnsureWechatPluginInstalled installs the WeChat OpenClaw plugin if needed (Wails UI).
func (s *OpenClawChannelService) EnsureWechatPluginInstalled() error {
	ctx, cancel := context.WithTimeout(context.Background(), wechatPluginInstallTimeout)
	defer cancel()
	return s.ensureWechatPluginInstalled(ctx)
}

// ilinkai API endpoints for WeChat QR code login.
// These are called directly — no OpenClaw WebSocket gateway needed for the login flow.
const (
	ilinkBaseURL    = "https://ilinkai.weixin.qq.com"
	ilinkBotType    = "3"
	ilinkGetQRPath  = "ilink/bot/get_bot_qrcode"
	ilinkStatusPath = "ilink/bot/get_qrcode_status"
)

// ilinkQRCodeResponse is the JSON response from the ilinkai get_bot_qrcode endpoint.
type ilinkQRCodeResponse struct {
	QRCode          string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

// ilinkStatusResponse is the JSON response from the ilinkai get_qrcode_status endpoint.
type ilinkStatusResponse struct {
	Status       string `json:"status"` // wait | scaned | confirmed | expired | scaned_but_redirect
	BotToken     string `json:"bot_token"`
	IlinkBotID   string `json:"ilink_bot_id"`
	BaseURL      string `json:"baseurl"`
	IlinkUserID  string `json:"ilink_user_id"`
	RedirectHost string `json:"redirect_host"`
}

// weixinDirectLoginResult is the internal result of the direct ilinkai login polling.
type weixinDirectLoginResult struct {
	Connected bool
	AccountID string
	BotToken  string
	BaseURL   string
	UserID    string
	Message   string
}

// WechatQRCodeResult holds the QR code image and a session key for subsequent login polling.
type WechatQRCodeResult struct {
	QRCodeDataURL string `json:"qrcode_data_url"` // base64 data URL or direct HTTPS URL
	SessionKey    string `json:"session_key"`     // equals the ilinkai qrcode identifier
}

// WechatLoginResult holds the result of waiting for the WeChat QR code scan.
type WechatLoginResult struct {
	Connected bool   `json:"connected"`
	AccountID string `json:"account_id"` // normalised weixin bot ID (e.g. "abc-im-bot")
	Message   string `json:"message"`
}

// normalizeWeixinAccountID converts a raw weixin bot ID (e.g. "abc@im.bot") to a
// filesystem-safe ID (e.g. "abc-im-bot") matching the plugin's normalizeAccountId helper.
func normalizeWeixinAccountID(rawID string) string {
	r := strings.ReplaceAll(rawID, "@", "-")
	return strings.ReplaceAll(r, ".", "-")
}

// GenerateWechatQRCode installs the plugin if needed and obtains a new WeChat QR code
// by calling the ilinkai HTTP API directly (bypasses the OpenClaw WebSocket gateway).
// Returns the QR code as a base64 data URL and a session key for subsequent polling.
// This is a Wails-exposed method called by the frontend.
func (s *OpenClawChannelService) GenerateWechatQRCode() (*WechatQRCodeResult, error) {
	if err := s.ensureOpenClawReady(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), wechatPluginInstallTimeout)
	defer cancel()

	if err := s.ensureWechatPluginInstalled(ctx); err != nil {
		return nil, errs.Wrap("error.channel_connect_failed", err)
	}

	qrResp, err := s.fetchWechatQRCodeDirect()
	if err != nil {
		return nil, errs.Wrap("error.channel_connect_failed", err)
	}

	s.app.Logger.Info("openclaw: wechat QR code obtained via ilinkai API",
		"sessionKey", qrResp.QRCode, "loginURL", qrResp.QRCodeImgContent)

	// qrcode_img_content is a WeChat login URL (e.g. https://liteapp.weixin.qq.com/q/...)
	// that must be QR-encoded so the user can scan it with the WeChat app.
	// Generate a PNG QR code locally — no external image download needed.
	dataURL, err := encodeURLAsQRDataURL(qrResp.QRCodeImgContent)
	if err != nil {
		return nil, errs.Wrap("error.channel_connect_failed",
			fmt.Errorf("generate wechat QR code image: %w", err))
	}

	return &WechatQRCodeResult{
		QRCodeDataURL: dataURL,
		SessionKey:    qrResp.QRCode,
	}, nil
}

// encodeURLAsQRDataURL generates a QR code PNG from loginURL and returns it as a
// base64 data URL ("data:image/png;base64,...") suitable for <img src>.
func encodeURLAsQRDataURL(loginURL string) (string, error) {
	png, err := qrcode.Encode(loginURL, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// fetchWechatQRCodeDirect calls the ilinkai get_bot_qrcode endpoint and returns the
// QR code data. No OpenClaw gateway involvement — pure HTTP.
func (s *OpenClawChannelService) fetchWechatQRCodeDirect() (*ilinkQRCodeResponse, error) {
	url := fmt.Sprintf("%s/%s?bot_type=%s", ilinkBaseURL, ilinkGetQRPath, ilinkBotType)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch wechat QR code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch wechat QR code: HTTP %d", resp.StatusCode)
	}
	var qr ilinkQRCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, fmt.Errorf("decode wechat QR code response: %w", err)
	}
	if qr.QRCode == "" {
		return nil, fmt.Errorf("wechat QR code response missing qrcode field")
	}
	return &qr, nil
}

// WaitForWechatLogin polls the ilinkai API (up to ~5 minutes) for the user scanning
// the QR code. On success:
//  1. Credentials are persisted to the plugin's file storage.
//  2. A local channel record is created.
//  3. A default agent is automatically created and bound to the channel.
//  4. A routing binding is written to openclaw.json.
//  5. The gateway is restarted once to load credentials + binding.
//
// This is a Wails-exposed method called by the frontend.
func (s *OpenClawChannelService) WaitForWechatLogin(sessionKey string, channelName string) (*WechatLoginResult, error) {
	if err := s.ensureOpenClawReady(); err != nil {
		return nil, err
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), wechatLoginWaitTimeout)
	defer cancel()

	lr, err := s.pollWechatLoginDirect(waitCtx, sessionKey)
	if err != nil {
		return nil, errs.Wrap("error.channel_connect_failed", err)
	}

	result := &WechatLoginResult{
		Connected: lr.Connected,
		AccountID: lr.AccountID,
		Message:   lr.Message,
	}

	if !lr.Connected {
		return result, nil
	}

	// Step 1: Persist credentials to the plugin's file storage.
	if err := s.saveWechatCredentials(lr); err != nil {
		s.app.Logger.Warn("openclaw: failed to save wechat credentials", "error", err)
	} else {
		s.app.Logger.Info("openclaw: wechat credentials saved", "accountId", lr.AccountID)
	}

	// Step 2: Create a local channel record.
	name := strings.TrimSpace(channelName)
	if name == "" {
		name = "微信"
	}
	ch, chErr := s.channelSvc.CreateChannel(channels.CreateChannelInput{
		Platform:       channels.PlatformWechat,
		Name:           name,
		Avatar:         "",
		ConnectionType: channels.ConnTypeGateway,
		ExtraConfig:    fmt.Sprintf(`{"platform":"wechat","account_id":%q}`, lr.AccountID),
		OpenClawScope:  true,
	})
	if chErr != nil {
		s.app.Logger.Warn("openclaw: failed to create wechat channel record after login", "error", chErr)
	} else if ch != nil {
		s.app.Logger.Info("openclaw: wechat channel record created", "channel_id", ch.ID, "accountId", lr.AccountID)

		// Step 3: Auto-bind a default agent so the channel routes messages immediately.
		agentID, agentErr := s.EnsureAgentForChannel(ch.ID)
		if agentErr != nil {
			s.app.Logger.Warn("openclaw: failed to ensure agent for wechat channel", "channel_id", ch.ID, "error", agentErr)
		} else {
			s.app.Logger.Info("openclaw: wechat channel agent bound", "channel_id", ch.ID, "agentId", agentID)

			// Step 4: Write agent entry + routing binding atomically in one openclaw.json
			// write. Writing them separately would trigger two gateway auto-restarts, causing
			// the agents.create RPC goroutine to fail on a closed connection. By combining
			// both into one write, the gateway restarts exactly once and finds both the agent
			// and the binding already present in the config.
			bindCtx, bindCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer bindCancel()
			openclawAgentID := s.lookupOpenClawAgentID(bindCtx, agentID)
			if openclawAgentID != "" {
				// Remove any stale legacy binding (channel_{id}) first (separate write is ok
				// here since gateway debounces restarts; alternatively skip if no stale binding).
				_ = s.removeManagedRouteBinding(wechatPluginID, openClawWechatAccountID(ch.ID))

				agentName := strings.TrimSpace(name)
				if agentName == "" {
					agentName = openclawAgentID
				}
				if bindErr := s.upsertManagedAgentAndBinding(openclawAgentID, agentName, wechatPluginID, lr.AccountID); bindErr != nil {
					s.app.Logger.Warn("openclaw: failed to write wechat agent+binding", "error", bindErr)
				} else {
					s.app.Logger.Info("openclaw: wechat agent+binding written",
						"accountId", lr.AccountID, "openclawAgentId", openclawAgentID)
				}

				// Write channels.openclaw-weixin entry so the gateway recognises
				// the plugin as a configured channel and calls startAccount on
				// the next restart (mirrors what triggerWeixinChannelReload does
				// inside the plugin's own QR login flow).
				if cfgErr := s.upsertWechatChannelConfig(lr.AccountID, agentName); cfgErr != nil {
					s.app.Logger.Warn("openclaw: failed to write wechat channel config", "error", cfgErr)
				} else {
					s.app.Logger.Info("openclaw: wechat channel config written", "accountId", lr.AccountID)
				}
			}
		}
	}

	// Step 5: Restart gateway once — loads credentials AND the new binding together.
	if err := s.restartOpenClawGateway(); err != nil {
		s.app.Logger.Warn("openclaw: gateway restart after wechat login failed", "error", err)
	} else {
		time.Sleep(3 * time.Second)
	}

	// Step 6: Mark channel online.
	if ch != nil {
		setCtx, cancelSet := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelSet()
		if err := s.setChannelOnlineStatus(setCtx, ch.ID, true); err != nil {
			s.app.Logger.Warn("openclaw: failed to set wechat channel online", "channel_id", ch.ID, "error", err)
		}
	}

	return result, nil
}

// pollWechatLoginDirect long-polls the ilinkai get_qrcode_status endpoint until the
// user confirms the QR code login or the context expires.
func (s *OpenClawChannelService) pollWechatLoginDirect(
	ctx context.Context,
	qrcode string,
) (*weixinDirectLoginResult, error) {
	baseURL := ilinkBaseURL
	client := &http.Client{Timeout: 40 * time.Second}
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		pollURL := fmt.Sprintf("%s/%s?qrcode=%s", baseURL, ilinkStatusPath, qrcode)
		resp, err := client.Get(pollURL)
		if err != nil {
			s.app.Logger.Warn("openclaw: wechat login poll error, retrying", "error", err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}

		var status ilinkStatusResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if decodeErr != nil {
			s.app.Logger.Warn("openclaw: wechat login status decode error", "error", decodeErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}

		switch status.Status {
		case "confirmed":
			accountID := normalizeWeixinAccountID(status.IlinkBotID)
			return &weixinDirectLoginResult{
				Connected: true,
				AccountID: accountID,
				BotToken:  status.BotToken,
				BaseURL:   status.BaseURL,
				UserID:    status.IlinkUserID,
				Message:   "✅ 与微信连接成功！",
			}, nil
		case "expired":
			return &weixinDirectLoginResult{
				Connected: false,
				Message:   "二维码已过期，请重新生成。",
			}, nil
		case "scaned_but_redirect":
			if status.RedirectHost != "" {
				baseURL = fmt.Sprintf("https://%s", status.RedirectHost)
				s.app.Logger.Info("openclaw: wechat login IDC redirect", "newBaseURL", baseURL)
			}
		}

		// "wait" or "scaned": keep polling.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// saveWechatCredentials writes the weixin account credentials directly to the plugin's
// file storage so the OpenClaw gateway can load the account on the next restart.
//
// File layout (mirrors accounts.ts in the plugin):
//
//	{stateDir}/openclaw-weixin/accounts.json          — account ID index (JSON array)
//	{stateDir}/openclaw-weixin/accounts/{id}.json     — account data (token, baseUrl, …)
func (s *OpenClawChannelService) saveWechatCredentials(lr *weixinDirectLoginResult) error {
	stateDir, err := s.openclawManager.BundleStateDir()
	if err != nil {
		return fmt.Errorf("get state dir: %w", err)
	}
	if lr.AccountID == "" {
		return fmt.Errorf("wechat login: accountId is empty")
	}

	accountsDir := filepath.Join(stateDir, "openclaw-weixin", "accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		return fmt.Errorf("create accounts dir: %w", err)
	}

	type accountData struct {
		Token   string `json:"token,omitempty"`
		SavedAt string `json:"savedAt,omitempty"`
		BaseURL string `json:"baseUrl,omitempty"`
		UserID  string `json:"userId,omitempty"`
	}
	data := accountData{
		Token:   lr.BotToken,
		SavedAt: time.Now().UTC().Format(time.RFC3339),
		BaseURL: lr.BaseURL,
		UserID:  lr.UserID,
	}
	accountJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal account data: %w", err)
	}
	accountPath := filepath.Join(accountsDir, lr.AccountID+".json")
	if err := os.WriteFile(accountPath, accountJSON, 0o600); err != nil {
		return fmt.Errorf("write account file: %w", err)
	}

	// Update the account ID index.
	indexPath := filepath.Join(stateDir, "openclaw-weixin", "accounts.json")
	var ids []string
	if existing, readErr := os.ReadFile(indexPath); readErr == nil {
		_ = json.Unmarshal(existing, &ids)
	}
	found := false
	for _, id := range ids {
		if id == lr.AccountID {
			found = true
			break
		}
	}
	if !found {
		ids = append(ids, lr.AccountID)
		indexJSON, _ := json.MarshalIndent(ids, "", "  ")
		_ = os.WriteFile(indexPath, append(indexJSON, '\n'), 0o644)
	}
	return nil
}

// connectWechatViaPlugin enables the WeChat channel in OpenClaw config and marks it online.
func (s *OpenClawChannelService) connectWechatViaPlugin(id int64, m *channelModel) error {
	if m.AgentID == 0 {
		return errs.New("error.channel_connect_requires_agent")
	}

	ctx, cancel := context.WithTimeout(context.Background(), openClawChannelSyncTimeout)
	defer cancel()

	if err := s.ensureWechatPluginInstalled(ctx); err != nil {
		return errs.Wrap("error.channel_connect_failed", err)
	}

	// Use the plugin-native weixin account ID (ilink_bot_id stored in extra_config.account_id).
	// Fall back to the legacy "channel_{id}" key only for channels created before this field existed.
	accountID := extractWechatAccountID(m.ExtraConfig)
	legacyID := openClawWechatAccountID(id)
	if accountID == "" {
		accountID = legacyID
	}

	openclawAgentID := s.lookupOpenClawAgentID(ctx, m.AgentID)
	if openclawAgentID != "" {
		// Remove any stale binding that used the legacy "channel_{id}" key so there
		// is no duplicate routing entry with an ID that the plugin will never emit.
		if accountID != legacyID {
			if err := s.removeManagedRouteBinding(wechatPluginID, legacyID); err != nil {
				s.app.Logger.Warn("openclaw: failed to remove stale wechat legacy binding",
					"channelId", id, "legacyId", legacyID, "error", err)
			}
		}

		// Resolve agent name for the config entry (fall back to openclawAgentID if unavailable).
		agentName := openclawAgentID
		if agent, agErr := s.agentsSvc.GetAgent(m.AgentID); agErr == nil && agent != nil {
			if n := strings.TrimSpace(agent.Name); n != "" {
				agentName = n
			}
		}
		// Write agent + binding in one atomic openclaw.json update to avoid the race where
		// each individual write triggers a gateway restart, causing agents.create RPC to fail.
		if err := s.upsertManagedAgentAndBinding(openclawAgentID, agentName, wechatPluginID, accountID); err != nil {
			s.app.Logger.Warn("openclaw: failed to write wechat agent+binding", "channelId", id, "error", err)
		}

		// Ensure channels.openclaw-weixin entry exists so the gateway recognises
		// the plugin as a configured channel and calls startAccount on restart.
		if cfgErr := s.upsertWechatChannelConfig(accountID, agentName); cfgErr != nil {
			s.app.Logger.Warn("openclaw: failed to write wechat channel config on reconnect", "channelId", id, "error", cfgErr)
		} else {
			s.app.Logger.Info("openclaw: wechat channel config written on reconnect", "channelId", id, "accountId", accountID)
		}

		if err := s.restartOpenClawGateway(); err != nil {
			return errs.Wrap("error.channel_connect_failed", err)
		}
	}

	return s.setChannelOnlineStatus(ctx, id, true)
}

// disconnectWechatViaPlugin marks the WeChat channel offline.
func (s *OpenClawChannelService) disconnectWechatViaPlugin(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), openClawChannelSyncTimeout)
	defer cancel()
	return s.setChannelOnlineStatus(ctx, id, false)
}

func (s *OpenClawChannelService) countEnabledWechatChannels(ctx context.Context, excludeID int64) int {
	db, err := s.db()
	if err != nil {
		return 0
	}
	listCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var models []channelModel
	if err := db.NewSelect().Model(&models).
		Where("ch.platform = ?", channels.PlatformWechat).
		Where("ch.enabled = ?", true).
		Where("ch.id != ?", excludeID).
		Where(openClawChannelVisibilitySQL).
		Scan(listCtx); err != nil {
		return 0
	}
	return len(models)
}

func openClawWechatAccountID(channelID int64) string {
	return fmt.Sprintf("channel_%d", channelID)
}


