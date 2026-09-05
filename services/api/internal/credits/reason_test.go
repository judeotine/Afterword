package credits

import "testing"

func TestReasonValidity(t *testing.T) {
	valid := []Reason{
		ReasonGrant, ReasonPurchase, ReasonBotUsage, ReasonTranscribeUsage,
		ReasonSummaryUsage, ReasonAskUsage, ReasonRefund, ReasonAdjust,
	}
	for _, reason := range valid {
		if !reason.Valid() {
			t.Errorf("Reason(%q).Valid() = false, want true", reason)
		}
	}
	for _, reason := range []Reason{"", "topup", "GRANT", "bot usage"} {
		if reason.Valid() {
			t.Errorf("Reason(%q).Valid() = true, want false", reason)
		}
	}
}

func TestUsageReasonsAreTheOnlyDebits(t *testing.T) {
	usage := []Reason{ReasonBotUsage, ReasonTranscribeUsage, ReasonSummaryUsage, ReasonAskUsage}
	for _, reason := range usage {
		if !reason.IsUsage() {
			t.Errorf("Reason(%q).IsUsage() = false, want true", reason)
		}
	}
	for _, reason := range []Reason{ReasonGrant, ReasonPurchase, ReasonRefund, ReasonAdjust, "nonsense"} {
		if reason.IsUsage() {
			t.Errorf("Reason(%q).IsUsage() = true, want false", reason)
		}
	}
}
