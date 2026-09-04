package api

import (
	"errors"
	"net/http"

	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const insufficientCreditsMessage = "This workspace does not have enough credits for that."

type entitlementErrorBody struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	TopUpURL string `json:"top_up_url"`
}

type entitlementErrorEnvelope struct {
	Error entitlementErrorBody `json:"error"`
}

func writeInsufficientCredits(w http.ResponseWriter, r *http.Request, topUpURL string) {
	httpx.WriteJSON(w, r, http.StatusPaymentRequired, entitlementErrorEnvelope{
		Error: entitlementErrorBody{
			Code:     codeInsufficient,
			Message:  insufficientCreditsMessage,
			TopUpURL: topUpURL,
		},
	})
}

func topUpURLFor(err error, fallback string) string {
	var insufficient *billing.InsufficientCreditsError
	if errors.As(err, &insufficient) && insufficient.TopUpURL != "" {
		return insufficient.TopUpURL
	}
	return fallback
}
