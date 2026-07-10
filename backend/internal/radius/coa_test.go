package radius

import (
	"context"
	"errors"
	"testing"
	"time"

	"layeh.com/radius"
)

func testCoA(exchange func(ctx context.Context, p *radius.Packet, addr string) (*radius.Packet, error)) *coaService {
	return &coaService{log: discardLogger(), now: func() time.Time { return fixedNow }, exchange: exchange}
}

func reply(code radius.Code) *radius.Packet { return radius.New(code, []byte("s")) }

func TestCoAAckNak(t *testing.T) {
	c := testCoA(func(context.Context, *radius.Packet, string) (*radius.Packet, error) {
		return reply(radius.CodeCoAACK), nil
	})
	res := c.exchangeWithRetry(context.Background(), reply(radius.CodeCoARequest), "1.2.3.4:3799", radius.CodeCoARequest)
	if !res.Ok() {
		t.Fatalf("want ACK, got %+v", res)
	}

	c = testCoA(func(context.Context, *radius.Packet, string) (*radius.Packet, error) {
		return reply(radius.CodeCoANAK), nil
	})
	res = c.exchangeWithRetry(context.Background(), reply(radius.CodeCoARequest), "1.2.3.4:3799", radius.CodeCoARequest)
	if res.Outcome != CoANAK || res.Err == nil {
		t.Fatalf("want NAK, got %+v", res)
	}
}

func TestCoATimeoutThenRetrySucceeds(t *testing.T) {
	var calls int
	c := testCoA(func(context.Context, *radius.Packet, string) (*radius.Packet, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return reply(radius.CodeDisconnectACK), nil
	})
	res := c.exchangeWithRetry(context.Background(), reply(radius.CodeDisconnectRequest), "1.2.3.4:3799", radius.CodeDisconnectRequest)
	if !res.Ok() {
		t.Fatalf("want ACK after retry, got %+v (calls=%d)", res, calls)
	}
	if calls != 2 {
		t.Fatalf("expected exactly one retry, got %d calls", calls)
	}
}

func TestCoATimeoutExhausted(t *testing.T) {
	c := testCoA(func(context.Context, *radius.Packet, string) (*radius.Packet, error) {
		return nil, context.DeadlineExceeded
	})
	res := c.exchangeWithRetry(context.Background(), reply(radius.CodeCoARequest), "1.2.3.4:3799", radius.CodeCoARequest)
	if res.Outcome != CoATimeout {
		t.Fatalf("want timeout, got %+v", res)
	}
}

func TestCoAExchangeError(t *testing.T) {
	c := testCoA(func(context.Context, *radius.Packet, string) (*radius.Packet, error) {
		return nil, errors.New("network down")
	})
	res := c.exchangeWithRetry(context.Background(), reply(radius.CodeCoARequest), "1.2.3.4:3799", radius.CodeCoARequest)
	if res.Outcome != CoAError {
		t.Fatalf("want error, got %+v", res)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 5: "5", 3799: "3799", -1: "-1"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
