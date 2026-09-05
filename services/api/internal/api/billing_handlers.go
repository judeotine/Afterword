package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/payments"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

const (
	maxWebhookBody          = 1 << 18
	codeInsufficient        = "insufficient_credits"
	codeProviderUnusable    = "provider_unavailable"
	codePaymentsUnavailable = "payments_unavailable"
	codeSignatureInvalid    = "signature_invalid"
	maxAdjustMinutes        = 1 << 20
)

var billingErrors = []struct {
	target error
	result statusError
}{
	{billing.ErrPackNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That credit pack is not available."}},
	{billing.ErrPaymentNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That payment does not exist."}},
	{billing.ErrInvalidPhone, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That phone number is not usable."}},
	{billing.ErrInvalidCursor, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That cursor is not valid."}},
	{billing.ErrProviderNotUsable, statusError{http.StatusServiceUnavailable, codePaymentsUnavailable, "Payments are not available right now."}},
	{payments.ErrProviderUnknown, statusError{http.StatusNotFound, httpx.CodeNotFound, "That payment provider is not known."}},
	{payments.ErrInvalidSignature, statusError{http.StatusUnauthorized, codeSignatureInvalid, "That webhook signature is not valid."}},
	{payments.ErrInvalidWebhook, statusError{http.StatusBadRequest, httpx.CodeInvalidRequest, "That webhook body could not be read."}},
	{payments.ErrProviderFailed, statusError{http.StatusBadGateway, codeProviderUnusable, "The payment provider could not be reached."}},
	{payments.ErrProviderRejected, statusError{http.StatusBadGateway, codeProviderUnusable, "The payment provider refused that request."}},
	{payments.ErrNotConfigured, statusError{http.StatusServiceUnavailable, codePaymentsUnavailable, "Payments are not configured."}},
	{payments.ErrRefundUnsupported, statusError{http.StatusNotImplemented, codeNotConfigured, "Refunds are not available for that provider."}},
	{credits.ErrInsufficientCredits, statusError{http.StatusPaymentRequired, codeInsufficient, "This workspace does not have enough credits."}},
	{credits.ErrInvalidAmount, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That is not a usable number of minutes."}},
	{credits.ErrInvalidReason, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That ledger reason is not allowed here."}},
	{credits.ErrWorkspaceNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That workspace does not exist."}},
}

func (b *BillingServer) writeBillingError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, billing.ErrCheckoutLimited) {
		writeRateLimited(w, r, b.billing.CheckoutWindow(),
			"Too many top-ups were started for this workspace: wait a while before trying again.")
		return
	}
	for _, candidate := range billingErrors {
		if errors.Is(err, candidate.target) {
			httpx.WriteError(w, r, candidate.result.status, candidate.result.code, candidate.result.message)
			return
		}
	}
	hlog.FromRequest(r).Error().Err(err).Msg("billing request failed")
	httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternalError, genericInternalMessage)
}

func (b *BillingServer) workspace(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		hlog.FromRequest(r).Error().Msg("a billing route ran without require workspace")
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return uuid.Nil, false
	}
	return membership.WorkspaceID, true
}

func (b *BillingServer) handleListPacks(w http.ResponseWriter, r *http.Request) {
	packs, err := b.billing.ListPacks(r.Context())
	if err != nil {
		b.writeBillingError(w, r, err)
		return
	}

	view := packListView{Packs: make([]packView, 0, len(packs))}
	for _, pack := range packs {
		view.Packs = append(view.Packs, newPackView(pack))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (b *BillingServer) handleBillingBalance(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := b.workspace(w, r)
	if !ok {
		return
	}

	balance, err := b.billing.Balance(r.Context(), workspaceID)
	if err != nil {
		b.writeBillingError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newBalanceView(balance))
}

type checkoutRequest struct {
	PackID string `json:"pack_id"`
	Phone  string `json:"phone"`
}

func (b *BillingServer) handleCheckout(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := b.workspace(w, r)
	if !ok {
		return
	}

	var body checkoutRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	packID := v.UUID("pack_id", body.PackID)
	phone := v.OptionalString("phone", body.Phone, billing.MaxPhoneLength)
	if v.Write(w, r) {
		return
	}

	checkout, err := b.billing.Checkout(r.Context(), workspaceID, packID, phone)
	if err != nil {
		b.writeBillingError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, newCheckoutView(checkout))
}

func (b *BillingServer) handleListPayments(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := b.workspace(w, r)
	if !ok {
		return
	}

	v := validation.New()
	pageSize := queryInt32(v, r, "limit", billing.DefaultPageSize, billing.MaxPageSize)
	if v.Write(w, r) {
		return
	}

	history, next, err := b.billing.ListPayments(r.Context(), workspaceID, queryString(r, "cursor"), pageSize)
	if err != nil {
		b.writeBillingError(w, r, err)
		return
	}

	view := paymentListView{Payments: make([]paymentView, 0, len(history)), NextCursor: next}
	for _, payment := range history {
		view.Payments = append(view.Payments, newPaymentView(payment))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (b *BillingServer) handleBillingWebhook(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, providerParam)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeInvalidRequest, "That webhook body could not be read.")
		return
	}

	result, err := b.billing.HandleWebhook(r.Context(), provider, r.Header, body)
	if err != nil {
		if errors.Is(err, billing.ErrWebhookUnmatched) {
			httpx.WriteJSON(w, r, http.StatusAccepted, webhookView{Received: true})
			return
		}
		b.writeBillingError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, webhookView{
		Received:    true,
		Status:      result.Payment.Status,
		Duplicate:   result.Duplicate,
		NeedsReview: result.NeedsReview,
	})
}

type adminAdjustRequest struct {
	DeltaMinutes int32  `json:"delta_minutes"`
	RefID        string `json:"ref_id"`
}

func (b *BillingServer) handleAdminAdjust(w http.ResponseWriter, r *http.Request) {
	if !b.adminAuthorized(w, r) {
		return
	}

	workspaceID, ok := pathUUID(r, workspaceParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That workspace does not exist.")
		return
	}

	var body adminAdjustRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	if body.DeltaMinutes == 0 {
		v.Add("delta_minutes", "must not be zero")
	}
	v.Min("delta_minutes", int64(body.DeltaMinutes), -maxAdjustMinutes)
	v.Max("delta_minutes", int64(body.DeltaMinutes), maxAdjustMinutes)
	refID := v.OptionalString("ref_id", body.RefID, 200)
	if v.Write(w, r) {
		return
	}

	balance, err := b.billing.Adjust(r.Context(), workspaceID, body.DeltaMinutes, refID)
	if err != nil {
		b.writeBillingError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newBalanceView(balance))
}
