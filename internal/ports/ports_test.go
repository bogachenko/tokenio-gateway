package ports_test

import (
	"context"
	"go/types"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type apiKeyRepositoryFake struct{}

func (apiKeyRepositoryFake) FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error) {
	return nil, nil
}

type userRepositoryFake struct{}

func (userRepositoryFake) FindByID(ctx context.Context, userID string) (*domain.User, error) {
	return nil, nil
}

type routeRepositoryFake struct{}

func (routeRepositoryFake) FindRoutes(ctx context.Context, query ports.RouteQuery) ([]domain.Route, error) {
	return nil, nil
}

type routePriceRepositoryFake struct{}

func (routePriceRepositoryFake) FindByRouteIDs(ctx context.Context, routeIDs []string) (map[string]domain.RoutePrice, error) {
	return nil, nil
}

type secretResolverFake struct{}

func (secretResolverFake) Resolve(ctx context.Context, name string) (string, error) { return "", nil }

type clockFake struct{}

func (clockFake) Now() time.Time { return time.Time{} }

type idGeneratorFake struct{}

func (idGeneratorFake) NewLocalRequestID() string         { return "" }
func (idGeneratorFake) NewAdminRequestID() string         { return "" }
func (idGeneratorFake) NewBillingChargeRequestID() string { return "" }

type billingIdentityServiceFake struct{}

func (billingIdentityServiceFake) TokenForSubject(ctx context.Context, billingSubjectUserID string) (string, error) {
	return "", nil
}

type billingBalanceClientFake struct{}

func (billingBalanceClientFake) GetBalance(ctx context.Context, billingToken string) (ports.BillingBalance, error) {
	return ports.BillingBalance{}, nil
}

type billingChargeClientFake struct{}

func (billingChargeClientFake) Charge(ctx context.Context, request ports.BillingChargeRequest) (ports.BillingChargeResult, error) {
	return ports.BillingChargeResult{}, nil
}

type tokenEstimatorFake struct{}

func (tokenEstimatorFake) Estimate(ctx context.Context, request ports.TokenEstimateRequest) (ports.TokenEstimate, error) {
	return ports.TokenEstimate{}, nil
}

type usageExtractorFake struct{}

func (usageExtractorFake) Extract(ctx context.Context, request ports.UsageExtractionRequest) (ports.UsageExtractionResult, error) {
	return ports.UsageExtractionResult{}, nil
}

type forwardingAdapterFake struct{}

func (forwardingAdapterFake) Forward(ctx context.Context, request ports.ForwardRequest) (ports.ForwardResponse, error) {
	return ports.ForwardResponse{}, nil
}

type usageLedgerFake struct{}

func (usageLedgerFake) CreateReserved(ctx context.Context, record domain.UsageRecord) (ports.UsageReserveResult, error) {
	return ports.UsageReserveResult{}, nil
}
func (usageLedgerFake) FindByLocalRequestID(ctx context.Context, localRequestID string) (*domain.UsageRecord, error) {
	return nil, nil
}
func (usageLedgerFake) CompareAndSwap(ctx context.Context, localRequestID string, expectedStatus domain.UsageStatus, next domain.UsageRecord) (ports.UsageTransitionResult, error) {
	return ports.UsageTransitionResult{}, nil
}
func (usageLedgerFake) LoadExposure(ctx context.Context, userID string, currency string) (ports.UsageExposureSnapshot, error) {
	return ports.UsageExposureSnapshot{}, nil
}

var (
	_ ports.APIKeyRepository       = apiKeyRepositoryFake{}
	_ ports.UserRepository         = userRepositoryFake{}
	_ ports.RouteRepository        = routeRepositoryFake{}
	_ ports.RoutePriceRepository   = routePriceRepositoryFake{}
	_ ports.SecretResolver         = secretResolverFake{}
	_ ports.Clock                  = clockFake{}
	_ ports.IDGenerator            = idGeneratorFake{}
	_ ports.BillingIdentityService = billingIdentityServiceFake{}
	_ ports.BillingBalanceClient   = billingBalanceClientFake{}
	_ ports.BillingChargeClient    = billingChargeClientFake{}
	_ ports.TokenEstimator         = tokenEstimatorFake{}
	_ ports.UsageExtractor         = usageExtractorFake{}
	_ ports.ForwardingAdapter      = forwardingAdapterFake{}
	_ ports.UsageLedger            = usageLedgerFake{}
)

func TestUsageLedgerPortExposesAtomicReservedAndCASContracts(t *testing.T) {
	ledgerType := reflect.TypeOf((*ports.UsageLedger)(nil)).Elem()
	for _, methodName := range []string{"CreateReserved", "FindByLocalRequestID", "CompareAndSwap", "LoadExposure"} {
		if _, ok := ledgerType.MethodByName(methodName); !ok {
			t.Fatalf("UsageLedger.%s is missing", methodName)
		}
	}

	if ports.UsageReserveOutcomeCreated != "created" || ports.UsageReserveOutcomeLocalRequestExists != "local_request_exists" || ports.UsageReserveOutcomeIdempotencyExists != "idempotency_exists" || ports.UsageReserveOutcomeUnresolvedUsage != "unresolved_usage" {
		t.Fatal("UsageLedger reserve outcomes changed")
	}

	reserveResultType := reflect.TypeOf(ports.UsageReserveResult{})
	if _, ok := reserveResultType.FieldByName("Existing"); !ok {
		t.Fatal("UsageReserveResult.Existing is missing")
	}
	transitionResultType := reflect.TypeOf(ports.UsageTransitionResult{})
	if _, ok := transitionResultType.FieldByName("Applied"); !ok {
		t.Fatal("UsageTransitionResult.Applied is missing")
	}
}

func TestRepositoryPortsUseContextAndAPIKeyRepositoryAcceptsHash(t *testing.T) {
	method, ok := reflect.TypeOf((*ports.APIKeyRepository)(nil)).Elem().MethodByName("FindByHash")
	if !ok {
		t.Fatal("FindByHash is missing")
	}
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if method.Type.In(0) != contextType {
		t.Fatalf("first argument = %v, want context.Context", method.Type.In(0))
	}
	if method.Type.In(1).Kind() != reflect.String {
		t.Fatalf("second argument = %v, want string hash", method.Type.In(1))
	}
	if method.Name != "FindByHash" {
		t.Fatalf("method name = %q, want hash-oriented lookup", method.Name)
	}

	userMethod, ok := reflect.TypeOf((*ports.UserRepository)(nil)).Elem().MethodByName("FindByID")
	if !ok {
		t.Fatal("FindByID is missing")
	}
	if userMethod.Type.In(0) != contextType {
		t.Fatalf("user repository first argument = %v, want context.Context", userMethod.Type.In(0))
	}
}

func TestBillingPortsExposeNormalizedContracts(t *testing.T) {
	chargeType := reflect.TypeOf(ports.BillingChargeRequest{})
	for _, forbidden := range []string{"ServiceToken", "Header", "URL"} {
		if _, ok := chargeType.FieldByName(forbidden); ok {
			t.Fatalf("BillingChargeRequest exposes forbidden field %s", forbidden)
		}
	}

	method, ok := reflect.TypeOf((*ports.BillingChargeClient)(nil)).Elem().MethodByName("Charge")
	if !ok {
		t.Fatal("Charge is missing")
	}
	if method.Type.NumIn() != 2 {
		t.Fatalf("Charge input count = %d, want context and normalized request", method.Type.NumIn())
	}
}

func TestForwardingPortDoesNotUseHTTPConcreteTypes(t *testing.T) {
	_ = types.NewPackage("github.com/bogachenko/tokenio-gateway/internal/ports", "ports")

	for _, typ := range []reflect.Type{reflect.TypeOf(ports.ForwardRequest{}), reflect.TypeOf(ports.ForwardResponse{})} {
		for i := 0; i < typ.NumField(); i++ {
			fieldPkgPath := packagePath(typ.Field(i).Type)
			if fieldPkgPath == "net/"+"http" {
				t.Fatalf("%s.%s uses concrete HTTP type %v", typ.Name(), typ.Field(i).Name, typ.Field(i).Type)
			}
		}
	}
}

func TestPortsPackageDoesNotImportForbiddenPackages(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "github.com/bogachenko/tokenio-gateway/internal/ports")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ports dependencies: %v", err)
	}
	deps := strings.Fields(string(out))
	sort.Strings(deps)
	joined := "\n" + strings.Join(deps, "\n") + "\n"
	for _, forbidden := range []string{
		"github.com/bogachenko/tokenio-gateway/internal/" + "billing",
		"github.com/bogachenko/tokenio-gateway/internal/" + "forwarding",
		"github.com/bogachenko/tokenio-gateway/internal/" + "config",
		"github.com/bogachenko/tokenio-gateway/internal/" + "httpapi",
		"net/" + "http",
		"database/" + "sql",
	} {
		if strings.Contains(joined, "\n"+forbidden+"\n") {
			t.Fatalf("ports imports forbidden package %s", forbidden)
		}
	}
}

func packagePath(typ reflect.Type) string {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map || typ.Kind() == reflect.Array {
		if typ.Kind() == reflect.Map {
			keyPath := packagePath(typ.Key())
			if keyPath != "" {
				return keyPath
			}
			typ = typ.Elem()
			continue
		}
		typ = typ.Elem()
	}
	if typ.PkgPath() != "" {
		return typ.PkgPath()
	}
	return ""
}
