package monitorsvc

import (
	"net/http"
	"time"
)

// httpClient is the shared client for the Telegram/WhatsApp senders: a bounded
// timeout so a hung external endpoint can never stall a dispatch (NFR-7).
func httpClient() *http.Client {
	return &http.Client{Timeout: 8 * time.Second}
}
