package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const (
	AdminTokenHeader = "X-Admin-Token"

	billingPacksPath    = "/v1/billing/packs"
	billingBalancePath  = "/v1/billing/balance"
	billingCheckoutPath = "/v1/billing/checkout"
	billingPaymentsPath = "/v1/billing/payments"
	billingWebhookPath  = "/v1/billing/webhooks/{" + providerParam + "}"
	adminAdjustPath     = "/v1/admin/workspaces/{" + workspaceParam + "}/credits/adjust"

	providerParam = "provider"
)

type BillingOptions struct {
	Billing    *billing.Service
	Middleware *auth.Middleware
	AdminToken string
	AppBaseURL string
}

type BillingServer struct {
	billing    *billing.Service
	middleware *auth.Middleware
	adminToken []byte
	appBaseURL string
}

func NewBillingServer(options BillingOptions) (*BillingServer, error) {
	switch {
	case options.Billing == nil:
		return nil, errors.New("api: a billing service is required")
	case options.Middleware == nil:
		return nil, errors.New("api: auth middleware is required")
	}

	server := &BillingServer{
		billing:    options.Billing,
		middleware: options.Middleware,
		appBaseURL: strings.TrimRight(strings.TrimSpace(options.AppBaseURL), "/"),
	}
	if token := strings.TrimSpace(options.AdminToken); token != "" {
		server.adminToken = []byte(token)
	}
	return server, nil
}

func (b *BillingServer) Routes(router chi.Router) {
	router.Post(billingWebhookPath, b.handleBillingWebhook)
	router.Post(adminAdjustPath, b.handleAdminAdjust)

	router.Group(func(r chi.Router) {
		r.Use(b.middleware.RequireAuth)
		r.Get(billingPacksPath, b.handleListPacks)

		r.Group(func(r chi.Router) {
			r.Use(b.middleware.RequireWorkspace)
			r.Get(billingBalancePath, b.handleBillingBalance)

			r.Group(func(r chi.Router) {
				r.Use(b.middleware.RequireRole(auth.RoleAdmin))
				r.Post(billingCheckoutPath, b.handleCheckout)
				r.Get(billingPaymentsPath, b.handleListPayments)
			})
		})
	})
}

func RegisterBillingRoutes(router chi.Router, options BillingOptions) error {
	server, err := NewBillingServer(options)
	if err != nil {
		return err
	}
	server.Routes(router)
	return nil
}

func (b *BillingServer) adminAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if len(b.adminToken) == 0 {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "The requested resource does not exist.")
		return false
	}
	provided := []byte(strings.TrimSpace(r.Header.Get(AdminTokenHeader)))
	expectedDigest := sha256.Sum256(b.adminToken)
	providedDigest := sha256.Sum256(provided)
	if subtle.ConstantTimeCompare(expectedDigest[:], providedDigest[:]) != 1 {
		httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "That administration token is not valid.")
		return false
	}
	return true
}
