package postgres

import (
	"errors"
	"math"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestDecodeCapabilitiesStrictObject(t *testing.T) {
	got, err := decodeCapabilities([]byte(
		`{"chat":true,"tools":true,"reasoning":false}`,
	))
	if err != nil {
		t.Fatalf("decodeCapabilities: %v", err)
	}
	if !got.Chat || !got.Tools || got.Reasoning {
		t.Fatalf("decoded capabilities = %+v", got)
	}
}

func TestDecodeCapabilitiesRejectsUnknownField(t *testing.T) {
	_, err := decodeCapabilities([]byte(`{"chat":true,"unknown":true}`))
	if !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("error = %v, want ErrStoreContractViolation", err)
	}
}

func TestDecodeCapabilitiesRejectsNonObject(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`"chat"`),
	} {
		_, err := decodeCapabilities(raw)
		if !errors.Is(err, ports.ErrStoreContractViolation) {
			t.Fatalf(
				"decodeCapabilities(%s) error = %v, want contract violation",
				raw,
				err,
			)
		}
	}
}

func TestUniqueIDsPreservesFirstOccurrence(t *testing.T) {
	got := uniqueIDs([]string{"route-2", "route-1", "route-2"})
	if len(got) != 2 || got[0] != "route-2" || got[1] != "route-1" {
		t.Fatalf("uniqueIDs = %#v", got)
	}
}

func TestValidateRoutePricePersistenceRejectsInvalidMarkup(t *testing.T) {
	for _, markup := range []float64{
		0,
		-1,
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
	} {
		value := domain.RoutePrice{MarkupCoefficient: markup}
		if err := validateRoutePricePersistence(value); !errors.Is(
			err,
			ports.ErrStoreContractViolation,
		) {
			t.Fatalf(
				"markup %v error = %v, want contract violation",
				markup,
				err,
			)
		}
	}
}

func TestRepositoryConstructorsRejectNilDB(t *testing.T) {
	constructors := []func() error{
		func() error {
			_, err := NewUserRepository(nil)
			return err
		},
		func() error {
			_, err := NewAPIKeyRepository(nil)
			return err
		},
		func() error {
			_, err := NewResellerRepository(nil)
			return err
		},
		func() error {
			_, err := NewRouteRepository(nil)
			return err
		},
		func() error {
			_, err := NewRoutePriceRepository(nil)
			return err
		},
	}
	for index, constructor := range constructors {
		if err := constructor(); !errors.Is(err, ErrInvalidDatabaseConfig) {
			t.Fatalf(
				"constructor %d error = %v, want ErrInvalidDatabaseConfig",
				index,
				err,
			)
		}
	}
}
