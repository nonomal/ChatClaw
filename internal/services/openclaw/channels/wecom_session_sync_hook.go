package openclawchannels

import (
	"encoding/json"
	"strings"

	"chatclaw/internal/services/channels"
)

const wecomOpenClawSessionSyncListenerKey = "openclaw-wecom-session-sync"

// OnGatewayReadyWeComSessionSync registers a gateway listener so that when a WeCom
// plugin-managed OpenClaw run finishes, we mirror sessions.json into local conversations.
func (s *OpenClawChannelService) OnGatewayReadyWeComSessionSync() {
	if s == nil || s.openclawManager == nil {
		return
	}
	m := s.openclawManager
	m.RemoveEventListener(wecomOpenClawSessionSyncListenerKey)
	m.AddEventListener(wecomOpenClawSessionSyncListenerKey, func(event string, payload json.RawMessage) {
		s.handleGatewayEventForWeComSessionSync(event, payload)
	})
}

func (s *OpenClawChannelService) handleGatewayEventForWeComSessionSync(event string, payload json.RawMessage) {
	sessionKey := ""
	switch strings.TrimSpace(event) {
	case "agent":
		var frame struct {
			SessionKey string `json:"sessionKey"`
		}
		if json.Unmarshal(payload, &frame) != nil || strings.TrimSpace(frame.SessionKey) == "" {
			return
		}
		sessionKey = frame.SessionKey
	case "chat":
		var frame struct {
			SessionKey string `json:"sessionKey"`
		}
		if json.Unmarshal(payload, &frame) != nil || strings.TrimSpace(frame.SessionKey) == "" {
			return
		}
		sessionKey = frame.SessionKey
	default:
		return
	}

	openclawAgentStr, platform, ok := parseOpenClawPluginSessionKeyPrefix(sessionKey)
	if !ok || !isWeComSessionPlatform(platform) {
		return
	}

	localID, err := s.agentsSvc.ResolveLocalIDByOpenClawAgentID(openclawAgentStr)
	if err != nil || localID <= 0 {
		return
	}

	go func(agentID int64, sk string) {
		if err := s.updateChannelLastReplyTargetBySessionKey(agentID, sk); err != nil {
			if s.app != nil {
				s.app.Logger.Warn("openclaw: wecom immediate last reply target update failed",
					"agentId", agentID, "sessionKey", sk, "error", err)
			}
			return
		}
		if s.app != nil {
			s.app.Logger.Info("openclaw: wecom immediate last reply target updated",
				"agentId", agentID, "sessionKey", sk)
		}
	}(localID, sessionKey)
}

func isWeComSessionPlatform(platform string) bool {
	return strings.EqualFold(strings.TrimSpace(platform), channels.PlatformWeCom)
}
