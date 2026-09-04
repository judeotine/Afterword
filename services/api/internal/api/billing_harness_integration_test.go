//go:build integration

package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/api"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/payments"
)

const (
	billingAdminToken = "admin-token-0123456789abcdefghij"
	billingFakeSecret = "fake-secret-0123456789ab"
)

type billingHarness struct {
	*harness
	pool    *pgxpool.Pool
	ledger  *credits.Ledger
	service *billing.Service
	fake    *payments.Fake
}

func newBillingHarness(t *testing.T) *billingHarness {
	t.Helper()
	pool := dbtest.New(t)

	store, err := auth.NewStore(pool)
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	accountsService, err := accounts.NewService(pool)
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	tokens, err := auth.NewTokenIssuer([]byte(harnessSecret))
	if err != nil {
		t.Fatalf("new token issuer: %v", err)
	}
	refresh, err := auth.NewRefreshManager(auth.RefreshManagerOptions{Store: store})
	if err != nil {
		t.Fatalf("new refresh manager: %v", err)
	}
	middleware, err := auth.NewMiddleware(auth.MiddlewareOptions{Issuer: tokens, Memberships: accountsService})
	if err != nil {
		t.Fatalf("new middleware: %v", err)
	}

	sender := &captureSender{}
	otp, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}

	fake, err := payments.NewFake(payments.FakeOptions{Secret: billingFakeSecret})
	if err != nil {
		t.Fatalf("new fake provider: %v", err)
	}
	registry := payments.NewRegistry()
	if err := registry.Register(payments.FakeProviderName, fake); err != nil {
		t.Fatalf("register fake provider: %v", err)
	}

	ledger := credits.NewLedger(pool)
	service, err := billing.NewService(billing.ServiceOptions{
		Pool:             pool,
		Ledger:           ledger,
		Providers:        registry,
		DefaultProvider:  payments.FakeProviderName,
		FreeGrantMinutes: 300,
		APIBaseURL:       "http://localhost:8080",
	})
	if err != nil {
		t.Fatalf("new billing service: %v", err)
	}

	server, err := api.NewServer(api.ServerOptions{
		Accounts:   accountsService,
		OTP:        otp,
		Tokens:     tokens,
		Refresh:    refresh,
		Middleware: middleware,
		Email:      sender,
		AppBaseURL: "http://localhost:3000",
	})
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}

	handler := httpx.NewRouter(httpx.RouterOptions{
		Logger:        zerolog.Nop(),
		AllowedOrigin: "http://localhost:3000",
		Mount: func(router chi.Router) {
			server.Routes(router)
			if err := api.RegisterBillingRoutes(router, api.BillingOptions{
				Billing:    service,
				Middleware: middleware,
				AdminToken: billingAdminToken,
				AppBaseURL: "http://localhost:3000",
			}); err != nil {
				t.Fatalf("register billing routes: %v", err)
			}
		},
	})

	return &billingHarness{
		harness: &harness{t: t, sender: sender, handler: handler},
		pool:    pool,
		ledger:  ledger,
		service: service,
		fake:    fake,
	}
}

func (h *billingHarness) context() context.Context {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	h.t.Cleanup(cancel)
	return ctx
}

func (h *billingHarness) balance(workspaceID uuid.UUID) int64 {
	h.t.Helper()
	balance, err := h.ledger.Balance(h.context(), workspaceID)
	if err != nil {
		h.t.Fatalf("read balance: %v", err)
	}
	return balance
}

func withAdminToken(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(api.AdminTokenHeader, token)
	}
}

func withFakeSignature(secret string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(payments.FakeSecretHeader, secret)
	}
}

type packsPayload struct {
	Packs []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Minutes    int32  `json:"minutes"`
		PriceMinor int64  `json:"price_minor"`
		Currency   string `json:"currency"`
	} `json:"packs"`
}

type balancePayload struct {
	Minutes          int64   `json:"minutes"`
	GrantExpiresAt   *string `json:"grant_expires_at"`
	FreeGrantMinutes int32   `json:"free_grant_minutes"`
}

type checkoutPayload struct {
	PaymentID   string `json:"payment_id"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Minutes     int32  `json:"minutes"`
	RedirectURL string `json:"redirect_url"`
	PushSent    bool   `json:"push_sent"`
}

type paymentsPayload struct {
	Payments []struct {
		ID          string `json:"id"`
		Provider    string `json:"provider"`
		Status      string `json:"status"`
		Minutes     int32  `json:"minutes"`
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	} `json:"payments"`
	NextCursor string `json:"next_cursor"`
}

type webhookPayload struct {
	Received  bool   `json:"received"`
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate"`
}
