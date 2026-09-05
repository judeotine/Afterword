package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/billing"
)

type packView struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Minutes    int32     `json:"minutes"`
	PriceMinor int64     `json:"price_minor"`
	Currency   string    `json:"currency"`
}

type packListView struct {
	Packs []packView `json:"packs"`
}

type balanceView struct {
	Minutes          int64      `json:"minutes"`
	GrantExpiresAt   *time.Time `json:"grant_expires_at"`
	FreeGrantMinutes int32      `json:"free_grant_minutes"`
}

type paymentView struct {
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	PackID      *uuid.UUID `json:"pack_id,omitempty"`
	Provider    string     `json:"provider"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	Minutes     int32      `json:"minutes"`
	Status      string     `json:"status"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type paymentListView struct {
	Payments   []paymentView `json:"payments"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type checkoutView struct {
	PaymentID   uuid.UUID `json:"payment_id"`
	Status      string    `json:"status"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	Minutes     int32     `json:"minutes"`
	RedirectURL string    `json:"redirect_url,omitempty"`
	PushSent    bool      `json:"push_sent"`
}

type webhookView struct {
	Received    bool   `json:"received"`
	Status      string `json:"status"`
	Duplicate   bool   `json:"duplicate"`
	NeedsReview bool   `json:"needs_review"`
}

func newPackView(pack billing.Pack) packView {
	return packView{
		ID:         pack.ID,
		Name:       pack.Name,
		Minutes:    pack.Minutes,
		PriceMinor: pack.PriceMinor,
		Currency:   pack.Currency,
	}
}

func newBalanceView(balance billing.Balance) balanceView {
	return balanceView{
		Minutes:          balance.Minutes,
		GrantExpiresAt:   balance.GrantExpiresAt,
		FreeGrantMinutes: balance.FreeGrantMinutes,
	}
}

func newPaymentView(payment billing.Payment) paymentView {
	return paymentView{
		ID:          payment.ID,
		WorkspaceID: payment.WorkspaceID,
		PackID:      payment.PackID,
		Provider:    payment.Provider,
		AmountMinor: payment.AmountMinor,
		Currency:    payment.Currency,
		Minutes:     payment.Minutes,
		Status:      payment.Status,
		PaidAt:      payment.PaidAt,
		CreatedAt:   payment.CreatedAt,
	}
}

func newCheckoutView(checkout billing.Checkout) checkoutView {
	return checkoutView{
		PaymentID:   checkout.Payment.ID,
		Status:      checkout.Payment.Status,
		AmountMinor: checkout.Payment.AmountMinor,
		Currency:    checkout.Payment.Currency,
		Minutes:     checkout.Payment.Minutes,
		RedirectURL: checkout.RedirectURL,
		PushSent:    checkout.PushSent,
	}
}
