package decision

import (
	"testing"

	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestProviderAllowedActionIsPositiveAndRuntimeOwnedActionsStayForbidden(t *testing.T) {
	allowed := []trace.ActionType{trace.ActionSelectMerchant, trace.ActionRetrySameMerchant, trace.ActionSwitchMerchant, trace.ActionRediscover, trace.ActionAskParent, trace.ActionStop}
	for _, action := range allowed {
		if !ProviderAllowedAction(action) {
			t.Fatalf("provider action %s was not allowlisted", action)
		}
	}
	for _, action := range []trace.ActionType{trace.ActionDiscover, trace.ActionInvoke, trace.ActionParse402, trace.ActionReserveBudget, trace.ActionCreatePayment, trace.ActionVerifyEntitlement, trace.ActionValidateDelivery, trace.ActionPaymentConfirmed, trace.ActionPaymentFailed, trace.ActionParentDecision, trace.ActionType("PARENT_APPROVED"), trace.ActionType("PARENT_DENIED"), trace.ActionType("ADJUST_BUDGET")} {
		if ProviderAllowedAction(action) {
			t.Fatalf("runtime-owned action %s was allowlisted", action)
		}
	}
}
