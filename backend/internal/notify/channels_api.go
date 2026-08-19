package notify

// Channels tab (FR-160): read-only health/status. Credentials themselves
// are edited through the existing GET/PUT /api/v1/settings/notifications
// (setupapi/settings_api.go), extended this phase with "sms" — see
// module.go's Register comment.

import (
	"context"
	"net/http"

	"github.com/hikrad/hikrad/internal/httpapi"
	"github.com/hikrad/hikrad/internal/platform"
)

type channelStatus struct {
	Channel    string `json:"channel"`
	Configured bool   `json:"configured"`
}

// channelStatuses reports, per channel, whether the settings group has a
// stored (encrypted) value — never the value itself.
func channelStatuses(ctx context.Context) []channelStatus {
	configured := func(key string) bool {
		_, err := platform.GetEncryptedRaw(ctx, pkgSettings, key)
		return err == nil
	}
	return []channelStatus{
		{Channel: string(ChannelInApp), Configured: true}, // always available (Redis publish)
		{Channel: string(ChannelTelegram), Configured: configured("notifications.telegram")},
		{Channel: string(ChannelEmail), Configured: configured("notifications.smtp")},
		{Channel: string(ChannelWhatsApp), Configured: configured("notifications.whatsapp")},
		{Channel: string(ChannelSMS), Configured: configured("notifications.sms")},
	}
}

func handleListChannels(w http.ResponseWriter, r *http.Request) {
	httpapi.JSON(w, http.StatusOK, channelStatuses(r.Context()))
}
