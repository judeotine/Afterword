package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/credits"
)

const (
	TopUpPath            = "/billing/top-up"
	DefaultBotEstimate   = int32(60)
	MaxEstimateMinutes   = int32(24 * 60)
	minimumEstimateValue = int32(1)
)

var ErrInvalidEstimate = errors.New("billing: the estimate must be a positive number of minutes")

type InsufficientCreditsError struct {
	Required int32
	Balance  int64
	TopUpURL string
}

func (e *InsufficientCreditsError) Error() string {
	return fmt.Sprintf("billing: %d minutes required, %d available", e.Required, e.Balance)
}

func (e *InsufficientCreditsError) Unwrap() error {
	return credits.ErrInsufficientCredits
}

type Entitlements struct {
	ledger   *credits.Ledger
	topUpURL string
}

func NewEntitlements(ledger *credits.Ledger, appBaseURL string) (*Entitlements, error) {
	if ledger == nil {
		return nil, errors.New("billing: a credit ledger is required")
	}
	return &Entitlements{
		ledger:   ledger,
		topUpURL: strings.TrimRight(strings.TrimSpace(appBaseURL), "/") + TopUpPath,
	}, nil
}

func (e *Entitlements) TopUpURL() string {
	return e.topUpURL
}

func (e *Entitlements) RequireCredits(ctx context.Context, workspaceID uuid.UUID, estimateMinutes int32) error {
	if estimateMinutes < minimumEstimateValue {
		return ErrInvalidEstimate
	}
	balance, err := e.ledger.Require(ctx, workspaceID, estimateMinutes)
	if err != nil {
		if errors.Is(err, credits.ErrInsufficientCredits) {
			return &InsufficientCreditsError{Required: estimateMinutes, Balance: balance, TopUpURL: e.topUpURL}
		}
		return err
	}
	return nil
}
