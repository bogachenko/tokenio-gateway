package ports

import (
	"reflect"
	"testing"
)

func TestAdministrativeMutationContractsCarryAuditAtomically(t *testing.T) {
	contracts := []struct {
		name    string
		typeOf  reflect.Type
		methods []string
	}{
		{"users", reflect.TypeOf((*AdminUserRepository)(nil)).Elem(), []string{"CreateUserWithAudit", "CompareAndSwapUserWithAudit"}},
		{"api keys", reflect.TypeOf((*AdminAPIKeyRepository)(nil)).Elem(), []string{"CreateAPIKeyWithAudit", "CompareAndSwapAPIKeyWithAudit"}},
		{"resellers", reflect.TypeOf((*ResellerRepository)(nil)).Elem(), []string{"CreateResellerWithAudit", "CompareAndSwapResellerWithAudit"}},
		{"routes", reflect.TypeOf((*AdminRouteRepository)(nil)).Elem(), []string{"CreateRouteWithAudit", "CompareAndSwapRouteWithAudit"}},
		{"prices", reflect.TypeOf((*AdminRoutePriceRepository)(nil)).Elem(), []string{"UpsertRoutePriceWithAudit"}},
		{"usage", reflect.TypeOf((*AdminUsageLedger)(nil)).Elem(), []string{"ResolvePricingFailedWithAudit", "ApplyChargeRetrySuccessWithAudit", "MarkChargeRetryFailedWithAudit"}},
	}
	for _, contract := range contracts {
		for _, method := range contract.methods {
			if _, ok := contract.typeOf.MethodByName(method); !ok {
				t.Fatalf("%s missing %s", contract.name, method)
			}
		}
	}
	if _, ok := reflect.TypeOf((*AdminAuditStore)(nil)).Elem().MethodByName("Append"); ok {
		t.Fatal("application must not append audit independently from mutation")
	}
}
