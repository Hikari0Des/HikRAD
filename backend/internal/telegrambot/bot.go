package telegrambot

// Long-poll loop (FR-165, NFR-7). Pull-based by construction: the bot asks
// Telegram for updates (getUpdates), so there is nothing to buffer on our
// side when Telegram is unreachable — a failed poll simply retries with
// backoff, which is what keeps this from ever growing an unbounded queue
// (gate item 12). When no bot token is configured, Run degrades to a
// silent, near-zero-cost no-op that periodically rechecks — the bot's
// absence never affects any other loop in hikrad-monitor (NFR-7).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hikrad/hikrad/internal/notify"
	"github.com/hikrad/hikrad/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	pollTimeoutSeconds  = 25
	configCheckInterval = 60 * time.Second
	maxBackoff          = 30 * time.Second
)

type bot struct {
	db       *pgxpool.Pool
	settings platform.Settings
	log      *slog.Logger
	client   *http.Client
	offset   int64
}

// Run drives the bot for the process lifetime. Safe to call unconditionally
// — it is a no-op loop until a bot token is configured.
func Run(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, settings platform.Settings, log *slog.Logger) {
	if db == nil {
		return
	}
	b := &bot{
		db: db, settings: settings, log: log,
		client: &http.Client{Timeout: time.Duration(pollTimeoutSeconds+10) * time.Second},
	}
	b.run(ctx)
}

func (b *bot) run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		token := notify.TelegramBotToken(ctx, b.settings)
		if token == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(configCheckInterval):
			}
			continue
		}
		updates, err := b.getUpdates(ctx, token)
		if err != nil {
			if b.log != nil {
				b.log.Warn("telegrambot: getUpdates failed, backing off", "error", err, "backoff", backoff.String())
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, u := range updates {
			b.offset = u.UpdateID + 1
			b.handleUpdate(ctx, token, u)
		}
	}
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	Chat tgChat `json:"chat"`
	Text string `json:"text"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

func (b *bot) getUpdates(ctx context.Context, token string) ([]tgUpdate, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=%d", token, b.offset, pollTimeoutSeconds)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var out struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram: getUpdates not ok")
	}
	return out.Result, nil
}

// reply sends a synchronous command response — NOT a queued notification
// (see notify.SendTelegramText's doc comment: this is the bot's own
// request/response turn inside an existing conversation, the same posture
// as any HTTP handler's response body, not a message that belongs in the
// outbox).
func (b *bot) reply(ctx context.Context, chatID int64, text string) {
	if err := notify.SendTelegramText(ctx, b.settings, fmt.Sprintf("%d", chatID), text); err != nil && b.log != nil {
		b.log.Warn("telegrambot: reply failed", "chat_id", chatID, "error", err)
	}
}
