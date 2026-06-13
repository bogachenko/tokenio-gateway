package modelcatalog

import (
	"context"
	"errors"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	catalogObject = "list"
	modelObject   = "model"
	modelOwner    = "tokenio"
)

type Dependencies struct {
	Routes         ports.ModelCatalogRouteRepository
	Resellers      ports.ResellerQueryRepository
	Prices         ports.RoutePriceRepository
	Secrets        ports.SecretPresenceChecker
	RewriteSupport ports.ModelIdentifierRewriteSupport
	Clock          ports.Clock
	Currency       string
}

type Service struct {
	deps Dependencies
}

type routeGroup struct {
	kind   domain.EndpointKind
	routes []domain.Route
}

type catalogCandidate struct {
	route         domain.Route
	pricing       Pricing
	referenceCost int64
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Routes == nil ||
		deps.Resellers == nil ||
		deps.Prices == nil ||
		deps.Secrets == nil ||
		deps.RewriteSupport == nil ||
		deps.Clock == nil ||
		!validOpaque(deps.Currency) {
		return nil, ErrInvalidInput
	}
	return &Service{deps: deps}, nil
}

func (s *Service) List(
	ctx context.Context,
	apiFamily domain.APIFamily,
) (Catalog, error) {
	if ctx == nil ||
		s == nil ||
		!validAPIFamily(apiFamily) {
		return Catalog{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return Catalog{}, err
	}

	now := s.deps.Clock.Now()
	if now.IsZero() {
		return Catalog{}, ErrCatalogUnavailable
	}
	now = now.UTC()

	routes, err := s.deps.Routes.ListModelCatalogRoutes(
		ctx,
		apiFamily,
	)
	if err != nil {
		return Catalog{}, mapDependencyError(err)
	}

	groups, routeIDs, resellerIDs, err :=
		groupCatalogRoutes(routes, apiFamily)
	if err != nil {
		return Catalog{}, err
	}

	resellers, err := s.deps.Resellers.FindByIDs(
		ctx,
		resellerIDs,
	)
	if err != nil {
		return Catalog{}, mapDependencyError(err)
	}
	if err := validateReturnedResellers(
		resellers,
		resellerIDs,
	); err != nil {
		return Catalog{}, err
	}

	prices, err := s.deps.Prices.FindByRouteIDs(
		ctx,
		routeIDs,
	)
	if err != nil {
		return Catalog{}, mapDependencyError(err)
	}
	if err := validateReturnedPrices(
		prices,
		routeIDs,
	); err != nil {
		return Catalog{}, err
	}

	modelIDs := make([]string, 0, len(groups))
	for modelID := range groups {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)

	secretPresence := make(map[string]bool)
	result := Catalog{
		Object: catalogObject,
		Data:   make([]Model, 0, len(modelIDs)),
	}

	for _, modelID := range modelIDs {
		group := groups[modelID]
		model, err := s.buildModel(
			ctx,
			modelID,
			group,
			resellers,
			prices,
			secretPresence,
			now,
		)
		if err != nil {
			return Catalog{}, err
		}
		result.Data = append(result.Data, model)
	}
	return result, nil
}

func groupCatalogRoutes(
	routes []domain.Route,
	apiFamily domain.APIFamily,
) (
	map[string]routeGroup,
	[]string,
	[]string,
	error,
) {
	groups := make(map[string]routeGroup)
	routeIDs := make([]string, 0, len(routes))
	resellerIDs := make([]string, 0, len(routes))
	seenRoutes := make(map[string]struct{}, len(routes))
	seenResellers := make(map[string]struct{})

	for _, route := range routes {
		if err := validateRouteRecord(
			route,
			apiFamily,
		); err != nil {
			return nil, nil, nil, err
		}
		if _, exists := seenRoutes[route.ID]; exists {
			return nil, nil, nil, ErrCatalogUnavailable
		}
		seenRoutes[route.ID] = struct{}{}
		routeIDs = append(routeIDs, route.ID)

		if _, exists := seenResellers[route.ResellerID]; !exists {
			seenResellers[route.ResellerID] = struct{}{}
			resellerIDs = append(
				resellerIDs,
				route.ResellerID,
			)
		}

		group, exists := groups[route.ClientModel]
		if exists && group.kind != route.EndpointKind {
			return nil, nil, nil, ErrCatalogUnavailable
		}
		group.kind = route.EndpointKind
		group.routes = append(group.routes, route)
		groups[route.ClientModel] = group
	}

	sort.Strings(routeIDs)
	sort.Strings(resellerIDs)
	return groups, routeIDs, resellerIDs, nil
}

func (s *Service) buildModel(
	ctx context.Context,
	modelID string,
	group routeGroup,
	resellers map[string]domain.Reseller,
	prices map[string]domain.RoutePrice,
	secretPresence map[string]bool,
	now time.Time,
) (Model, error) {
	candidates := make(
		[]catalogCandidate,
		0,
		len(group.routes),
	)

	for _, route := range group.routes {
		reseller, exists := resellers[route.ResellerID]
		if !exists {
			return Model{}, ErrCatalogUnavailable
		}
		if err := validateResellerRecord(
			reseller,
			route,
		); err != nil {
			return Model{}, err
		}

		price, hasPrice := prices[route.ID]
		if hasPrice {
			if err := validatePriceRecord(
				price,
				route.ID,
			); err != nil {
				return Model{}, err
			}
		}

		if !hasPrice ||
			!catalogRouteStaticallyAvailable(
				route,
				reseller,
				price,
				s.deps.Currency,
				now,
			) ||
			!s.routeConfigurationSupported(route) {
			continue
		}

		exists, known := secretPresence[reseller.APIKeyEnv]
		if !known {
			var err error
			exists, err = s.deps.Secrets.Exists(
				ctx,
				reseller.APIKeyEnv,
			)
			if err != nil {
				return Model{}, mapDependencyError(err)
			}
			secretPresence[reseller.APIKeyEnv] = exists
		}
		if !exists {
			continue
		}

		pricing, err := publicPricing(price)
		if err != nil {
			return Model{}, err
		}
		referenceCost, ok := catalogReferenceCost(
			route.EndpointKind,
			pricing,
		)
		if !ok {
			continue
		}
		candidates = append(
			candidates,
			catalogCandidate{
				route:         route,
				pricing:       pricing,
				referenceCost: referenceCost,
			},
		)
	}

	model := Model{
		ID:           modelID,
		Object:       modelObject,
		OwnedBy:      modelOwner,
		Type:         string(group.kind),
		Active:       false,
		Pricing:      nil,
		Capabilities: domain.CapabilitySet{},
	}
	if len(candidates) == 0 {
		return model, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.referenceCost != right.referenceCost {
			return left.referenceCost < right.referenceCost
		}
		if left.route.Priority != right.route.Priority {
			return left.route.Priority < right.route.Priority
		}
		return left.route.ID < right.route.ID
	})

	selectedPricing := candidates[0].pricing
	model.Active = true
	model.Pricing = &selectedPricing
	for _, candidate := range candidates {
		model.Capabilities = unionCapabilities(
			model.Capabilities,
			candidate.route.Capabilities,
		)
	}
	return model, nil
}

func (s *Service) routeConfigurationSupported(
	route domain.Route,
) bool {
	if !endpointCapabilityPresent(route) {
		return false
	}

	switch route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		return route.ProviderModel == route.ClientModel
	case domain.ModelRewritePolicyProviderModel:
		return route.ProviderModel != "" &&
			s.deps.RewriteSupport.
				SupportsModelIdentifierRewrite(
					route.APIFamily,
					route.ProviderType,
				)
	default:
		return false
	}
}

func catalogRouteStaticallyAvailable(
	route domain.Route,
	reseller domain.Reseller,
	price domain.RoutePrice,
	currency string,
	now time.Time,
) bool {
	if !route.Enabled ||
		route.DisabledAt != nil ||
		!reseller.Enabled ||
		reseller.DisabledAt != nil ||
		!price.Enabled ||
		price.Currency != currency {
		return false
	}
	if route.CooldownUntil != nil &&
		route.CooldownUntil.After(now) {
		return false
	}
	return positiveAvailableBalance(reseller)
}

func positiveAvailableBalance(
	reseller domain.Reseller,
) bool {
	available := big.NewInt(reseller.BalanceCents)
	available.Sub(
		available,
		big.NewInt(reseller.ReservedCents),
	)
	available.Sub(
		available,
		big.NewInt(reseller.MinimumBalanceCents),
	)
	return available.Sign() > 0
}

func publicPricing(
	price domain.RoutePrice,
) (Pricing, error) {
	values := []int64{
		price.InputPricePer1MTokensCents,
		price.CachedInputPricePer1MTokensCents,
		price.OutputPricePer1MTokensCents,
		price.ReasoningOutputPricePer1MTokensCents,
		price.ImageInputPricePer1MTokensCents,
		price.AudioInputPricePer1MTokensCents,
		price.AudioOutputPricePer1MTokensCents,
		price.FileInputPricePer1MTokensCents,
		price.VideoInputPricePer1MTokensCents,
		price.ImageGenerationPricePerUnitCents,
	}
	markedUp := make([]int64, len(values))
	for index, value := range values {
		result, err := applyMarkup(
			value,
			price.MarkupCoefficient,
		)
		if err != nil {
			return Pricing{}, err
		}
		markedUp[index] = result
	}

	return Pricing{
		Currency:                             price.Currency,
		InputPricePer1MTokensCents:           markedUp[0],
		CachedInputPricePer1MTokensCents:     markedUp[1],
		OutputPricePer1MTokensCents:          markedUp[2],
		ReasoningOutputPricePer1MTokensCents: markedUp[3],
		ImageInputPricePer1MTokensCents:      markedUp[4],
		AudioInputPricePer1MTokensCents:      markedUp[5],
		AudioOutputPricePer1MTokensCents:     markedUp[6],
		FileInputPricePer1MTokensCents:       markedUp[7],
		VideoInputPricePer1MTokensCents:      markedUp[8],
		ImageGenerationPricePerUnitCents:     markedUp[9],
		ImageGenerationUnitKind:              price.ImageGenerationUnitKind,
	}, nil
}

func applyMarkup(
	value int64,
	markup float64,
) (int64, error) {
	if value < 0 ||
		markup <= 0 ||
		math.IsNaN(markup) ||
		math.IsInf(markup, 0) {
		return 0, ErrCatalogUnavailable
	}
	if value == 0 {
		return 0, nil
	}

	canonicalMarkup := strconv.FormatFloat(
		markup,
		'g',
		-1,
		64,
	)
	ratio, ok := new(big.Rat).SetString(
		canonicalMarkup,
	)
	if !ok || ratio.Sign() <= 0 {
		return 0, ErrCatalogUnavailable
	}

	numerator := new(big.Int).Mul(
		big.NewInt(value),
		ratio.Num(),
	)
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(
		numerator,
		ratio.Denom(),
		remainder,
	)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, ErrCatalogUnavailable
	}
	return quotient.Int64(), nil
}

func catalogReferenceCost(
	kind domain.EndpointKind,
	pricing Pricing,
) (int64, bool) {
	switch kind {
	case domain.EndpointChat:
		if pricing.InputPricePer1MTokensCents <= 0 ||
			pricing.OutputPricePer1MTokensCents <= 0 {
			return 0, false
		}
		total, ok := safeAdd(
			pricing.InputPricePer1MTokensCents,
			pricing.OutputPricePer1MTokensCents,
		)
		return total, ok

	case domain.EndpointEmbeddings:
		if pricing.InputPricePer1MTokensCents <= 0 {
			return 0, false
		}
		return pricing.InputPricePer1MTokensCents, true

	case domain.EndpointImagesGeneration:
		if pricing.ImageGenerationPricePerUnitCents <= 0 ||
			pricing.ImageGenerationUnitKind !=
				domain.ImageGenerationUnitKindGeneratedImage {
			return 0, false
		}
		return pricing.ImageGenerationPricePerUnitCents, true

	default:
		return 0, false
	}
}

func safeAdd(left, right int64) (int64, bool) {
	if left < 0 || right < 0 ||
		left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

func endpointCapabilityPresent(
	route domain.Route,
) bool {
	switch route.EndpointKind {
	case domain.EndpointChat:
		return route.Capabilities.Chat
	case domain.EndpointEmbeddings:
		return route.Capabilities.Embeddings
	case domain.EndpointImagesGeneration:
		return route.Capabilities.ImagesGeneration
	default:
		return false
	}
}

func unionCapabilities(
	left domain.CapabilitySet,
	right domain.CapabilitySet,
) domain.CapabilitySet {
	return domain.CapabilitySet{
		Chat: left.Chat || right.Chat,
		Embeddings: left.Embeddings ||
			right.Embeddings,
		ImagesGeneration: left.ImagesGeneration ||
			right.ImagesGeneration,
		Tools: left.Tools || right.Tools,
		ToolChoice: left.ToolChoice ||
			right.ToolChoice,
		ResponseFormat: left.ResponseFormat ||
			right.ResponseFormat,
		JSONSchema: left.JSONSchema ||
			right.JSONSchema,
		ImageInput: left.ImageInput ||
			right.ImageInput,
		AudioInput: left.AudioInput ||
			right.AudioInput,
		FileInput: left.FileInput ||
			right.FileInput,
		VideoInput: left.VideoInput ||
			right.VideoInput,
		Reasoning: left.Reasoning ||
			right.Reasoning,
	}
}

func validateRouteRecord(
	route domain.Route,
	apiFamily domain.APIFamily,
) error {
	if !validOpaque(route.ID) ||
		!validOpaque(route.ResellerID) ||
		!validOpaque(route.ClientModel) ||
		!validOpaque(route.ProviderModel) ||
		!validProviderType(route.ProviderType) ||
		route.APIFamily != apiFamily ||
		!validEndpointKind(route.EndpointKind) ||
		!validRewritePolicy(route.ModelRewritePolicy) ||
		route.Priority < 0 ||
		route.RequestsPerMinute < 0 ||
		route.TokensPerMinute < 0 ||
		route.ConcurrentRequests < 0 ||
		route.DefaultMaxOutputTokens < 0 ||
		route.Enabled != (route.DisabledAt == nil) ||
		!validOptionalUTC(route.DisabledAt) ||
		!validOptionalUTC(route.CooldownUntil) ||
		!validCooldownState(route) {
		return ErrCatalogUnavailable
	}
	return nil
}

func validateResellerRecord(
	reseller domain.Reseller,
	route domain.Route,
) error {
	if reseller.ID != route.ResellerID ||
		!validOpaque(reseller.ID) ||
		!validOpaque(reseller.BaseURL) ||
		!validOpaque(reseller.APIKeyEnv) ||
		!validProviderType(reseller.ProviderType) ||
		reseller.ProviderType != route.ProviderType ||
		reseller.ReservedCents < 0 ||
		reseller.MinimumBalanceCents < 0 ||
		reseller.Enabled != (reseller.DisabledAt == nil) ||
		!validOptionalUTC(reseller.DisabledAt) {
		return ErrCatalogUnavailable
	}
	return nil
}

func validatePriceRecord(
	price domain.RoutePrice,
	routeID string,
) error {
	values := []int64{
		price.InputPricePer1MTokensCents,
		price.CachedInputPricePer1MTokensCents,
		price.OutputPricePer1MTokensCents,
		price.ReasoningOutputPricePer1MTokensCents,
		price.ImageInputPricePer1MTokensCents,
		price.AudioInputPricePer1MTokensCents,
		price.AudioOutputPricePer1MTokensCents,
		price.FileInputPricePer1MTokensCents,
		price.VideoInputPricePer1MTokensCents,
		price.ImageGenerationPricePerUnitCents,
	}
	for _, value := range values {
		if value < 0 {
			return ErrCatalogUnavailable
		}
	}

	if price.RouteID != routeID ||
		!validOpaque(price.RouteID) ||
		!validOpaque(price.Currency) ||
		price.MarkupCoefficient <= 0 ||
		math.IsNaN(price.MarkupCoefficient) ||
		math.IsInf(price.MarkupCoefficient, 0) ||
		!validImageGenerationUnitKind(
			price.ImageGenerationUnitKind,
		) {
		return ErrCatalogUnavailable
	}
	return nil
}

func validateReturnedResellers(
	values map[string]domain.Reseller,
	requested []string,
) error {
	allowed := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		allowed[id] = struct{}{}
	}
	for id := range values {
		if _, exists := allowed[id]; !exists {
			return ErrCatalogUnavailable
		}
	}
	return nil
}

func validateReturnedPrices(
	values map[string]domain.RoutePrice,
	requested []string,
) error {
	allowed := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		allowed[id] = struct{}{}
	}
	for id := range values {
		if _, exists := allowed[id]; !exists {
			return ErrCatalogUnavailable
		}
	}
	return nil
}

func validOpaque(value string) bool {
	return value != "" &&
		value == strings.TrimSpace(value)
}

func validAPIFamily(value domain.APIFamily) bool {
	switch value {
	case domain.APIFamilyOpenAICompatible,
		domain.APIFamilyGeminiNative,
		domain.APIFamilyAnthropicNative,
		domain.APIFamilyOllamaNative:
		return true
	default:
		return false
	}
}

func validProviderType(value domain.ProviderType) bool {
	switch value {
	case domain.ProviderOpenAI,
		domain.ProviderOpenRouter,
		domain.ProviderTogether,
		domain.ProviderGroq,
		domain.ProviderOllama,
		domain.ProviderLMStudio,
		domain.ProviderVLLM,
		domain.ProviderGemini,
		domain.ProviderAnthropic,
		domain.ProviderHydra:
		return true
	default:
		return false
	}
}

func validEndpointKind(value domain.EndpointKind) bool {
	switch value {
	case domain.EndpointChat,
		domain.EndpointEmbeddings,
		domain.EndpointImagesGeneration:
		return true
	default:
		return false
	}
}

func validRewritePolicy(
	value domain.ModelRewritePolicy,
) bool {
	switch value {
	case domain.ModelRewritePolicyNone,
		domain.ModelRewritePolicyProviderModel:
		return true
	default:
		return false
	}
}

func validImageGenerationUnitKind(
	value domain.ImageGenerationUnitKind,
) bool {
	switch value {
	case domain.ImageGenerationUnitKindNone,
		domain.ImageGenerationUnitKindGeneratedImage:
		return true
	default:
		return false
	}
}

func validOptionalUTC(value *time.Time) bool {
	return value == nil ||
		(!value.IsZero() && value.Location() == time.UTC)
}

func validCooldownState(route domain.Route) bool {
	if route.CooldownUntil == nil {
		return route.CooldownReason == ""
	}
	return validOpaque(route.CooldownReason)
}

func mapDependencyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrCatalogUnavailable
}
