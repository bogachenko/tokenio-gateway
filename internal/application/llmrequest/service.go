package llmrequest

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

const localRequestIDPrefix = "llmreq_"

type Service struct {
	authenticator      Authenticator
	requestParser      RequestParser
	capabilityDetector CapabilityDetector
	routePlanner       RoutePlanner
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Authenticator == nil ||
		dependencies.RequestParser == nil ||
		dependencies.CapabilityDetector == nil ||
		dependencies.RoutePlanner == nil {
		return nil, ErrDependencyRequired
	}

	return &Service{
		authenticator:      dependencies.Authenticator,
		requestParser:      dependencies.RequestParser,
		capabilityDetector: dependencies.CapabilityDetector,
		routePlanner:       dependencies.RoutePlanner,
	}, nil
}

func (s *Service) Prepare(
	ctx context.Context,
	input Input,
) (PreparedRequest, error) {
	if s == nil ||
		s.authenticator == nil ||
		s.requestParser == nil ||
		s.capabilityDetector == nil ||
		s.routePlanner == nil {
		return PreparedRequest{}, ErrDependencyRequired
	}
	if ctx == nil {
		return PreparedRequest{}, fmt.Errorf(
			"%w: nil context",
			ErrInvalidInput,
		)
	}
	if err := validateInput(input); err != nil {
		return PreparedRequest{}, err
	}

	originalPayload := cloneBytes(input.Payload)

	principal, err := s.authenticator.Authenticate(
		ctx,
		input.RawAPIKey,
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"authenticate request: %w",
			err,
		)
	}
	if err := validatePrincipal(principal); err != nil {
		return PreparedRequest{}, err
	}

	parsed, err := s.requestParser.Parse(
		ctx,
		ParseInput{
			APIFamily:    input.APIFamily,
			EndpointKind: input.EndpointKind,
			Payload:      cloneBytes(originalPayload),
		},
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"parse request: %w",
			err,
		)
	}
	if strings.TrimSpace(parsed.ClientModel) == "" {
		return PreparedRequest{}, fmt.Errorf(
			"%w: parser returned blank client model",
			ErrStageContractViolation,
		)
	}

	requestedCapabilities, err := s.capabilityDetector.Detect(
		ctx,
		CapabilityInput{
			APIFamily:    input.APIFamily,
			EndpointKind: input.EndpointKind,
			ClientModel:  parsed.ClientModel,
			Payload:      cloneBytes(originalPayload),
		},
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"detect requested capabilities: %w",
			err,
		)
	}

	plan, err := s.routePlanner.Plan(
		ctx,
		RoutePlanInput{
			Principal:             principal,
			APIFamily:             input.APIFamily,
			EndpointKind:          input.EndpointKind,
			ClientModel:           parsed.ClientModel,
			RequestedCapabilities: requestedCapabilities,
			Payload:               cloneBytes(originalPayload),
		},
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"plan route: %w",
			err,
		)
	}
	if err := validateRoutePlan(
		input,
		parsed,
		plan,
	); err != nil {
		return PreparedRequest{}, err
	}

	return PreparedRequest{
		LocalRequestID:        input.LocalRequestID,
		IdempotencyKey:        cloneStringPointer(input.IdempotencyKey),
		Principal:             principal,
		APIFamily:             input.APIFamily,
		EndpointKind:          input.EndpointKind,
		ClientModel:           parsed.ClientModel,
		RequestedCapabilities: requestedCapabilities,
		Payload:               cloneBytes(originalPayload),
		Plan:                  plan,
	}, nil
}

func validateInput(input Input) error {
	if !validLocalRequestID(input.LocalRequestID) ||
		strings.TrimSpace(input.RawAPIKey) == "" ||
		input.APIFamily == "" ||
		input.EndpointKind == "" ||
		input.Payload == nil {
		return ErrInvalidInput
	}
	if input.IdempotencyKey != nil &&
		strings.TrimSpace(*input.IdempotencyKey) == "" {
		return fmt.Errorf(
			"%w: blank idempotency key",
			ErrInvalidInput,
		)
	}
	return nil
}

func validatePrincipal(value Principal) error {
	if strings.TrimSpace(value.UserID) == "" ||
		strings.TrimSpace(value.APIKeyID) == "" ||
		strings.TrimSpace(value.BillingSubjectUserID) == "" {
		return fmt.Errorf(
			"%w: invalid authentication principal",
			ErrStageContractViolation,
		)
	}
	return nil
}

func validateRoutePlan(
	input Input,
	parsed ParsedRequest,
	plan RoutePlan,
) error {
	route := plan.Route
	reseller := plan.Reseller
	price := plan.Price

	if strings.TrimSpace(route.ID) == "" ||
		strings.TrimSpace(route.ResellerID) == "" ||
		strings.TrimSpace(route.ClientModel) == "" ||
		strings.TrimSpace(route.ProviderModel) == "" ||
		strings.TrimSpace(reseller.ID) == "" ||
		strings.TrimSpace(price.RouteID) == "" ||
		route.APIFamily != input.APIFamily ||
		route.EndpointKind != input.EndpointKind ||
		route.ClientModel != parsed.ClientModel ||
		route.ResellerID != reseller.ID ||
		route.ProviderType == "" ||
		route.ProviderType != reseller.ProviderType ||
		price.RouteID != route.ID ||
		!route.Enabled ||
		!reseller.Enabled ||
		!price.Enabled ||
		price.Currency != "RUB" ||
		plan.Currency != "RUB" ||
		plan.EstimatedClientAmountCents < 0 ||
		plan.EstimatedUpstreamCostCents < 0 ||
		strings.TrimSpace(plan.Confidence) == "" ||
		!nonNegativeUsage(plan.EstimatedUsage) {
		return fmt.Errorf(
			"%w: invalid route plan",
			ErrStageContractViolation,
		)
	}
	return nil
}

func validLocalRequestID(value string) bool {
	if !strings.HasPrefix(value, localRequestIDPrefix) ||
		len(value) == len(localRequestIDPrefix) {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) ||
			unicode.IsSpace(current) {
			return false
		}
	}
	return true
}

func nonNegativeUsage(value domain.TokenUsage) bool {
	return value.InputTokens >= 0 &&
		value.CachedInputTokens >= 0 &&
		value.OutputTokens >= 0 &&
		value.ReasoningTokens >= 0 &&
		value.ImageInputTokens >= 0 &&
		value.AudioInputTokens >= 0 &&
		value.AudioOutputTokens >= 0 &&
		value.FileInputTokens >= 0 &&
		value.VideoInputTokens >= 0 &&
		value.ImageGenerationUnits >= 0
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
