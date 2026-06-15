package pricing

import (
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

const microCentsPerCent int64 = 1_000_000

var maxInt64Big = big.NewInt(math.MaxInt64)

type Calculator struct {
	tokenSafetyFactor *big.Rat
	costSafetyFactor  *big.Rat
}

type ActualCalculationInput struct {
	Usage      domain.TokenUsage
	Price      domain.RoutePrice
	InputMode  InputPricingMode
	Modalities InputModalities
}

type EstimateCalculationInput struct {
	Usage      domain.TokenUsage
	Price      domain.RoutePrice
	InputMode  InputPricingMode
	Modalities InputModalities
}

type MixedCalculationInput struct {
	ActualUsage    domain.TokenUsage
	EstimatedUsage domain.TokenUsage
	Price          domain.RoutePrice

	ActualInputMode    InputPricingMode
	EstimatedInputMode InputPricingMode
	Modalities         InputModalities
}

type CalculationResult struct {
	Usage domain.TokenUsage

	UpstreamCostCents int64
	ClientAmountCents int64
	Currency          string

	Estimated bool
}

func NewCalculator(tokenSafetyFactor float64, costSafetyFactor float64) (*Calculator, error) {
	token, err := positiveFiniteRat(tokenSafetyFactor, "token safety factor")
	if err != nil {
		return nil, err
	}
	cost, err := positiveFiniteRat(costSafetyFactor, "cost safety factor")
	if err != nil {
		return nil, err
	}
	return &Calculator{tokenSafetyFactor: token, costSafetyFactor: cost}, nil
}

func (c *Calculator) CalculateActual(input ActualCalculationInput) (CalculationResult, error) {
	if c == nil {
		return CalculationResult{}, fmt.Errorf("%w: nil calculator", ErrInvalidPricingInput)
	}
	return c.calculate(input.Usage, input.Price, input.InputMode, input.Modalities, false)
}

func (c *Calculator) CalculateEstimate(input EstimateCalculationInput) (CalculationResult, error) {
	if c == nil {
		return CalculationResult{}, fmt.Errorf("%w: nil calculator", ErrInvalidPricingInput)
	}
	usage, err := applyTokenSafetyFactor(input.Usage, c.tokenSafetyFactor)
	if err != nil {
		return CalculationResult{}, err
	}
	return c.calculate(usage, input.Price, input.InputMode, input.Modalities, true)
}

func (c *Calculator) CalculatePublicUnitPriceCents(
	unitPriceCents int64,
	markup float64,
) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf(
			"%w: nil calculator",
			ErrInvalidPricingInput,
		)
	}
	if unitPriceCents < 0 {
		return 0, fmt.Errorf(
			"%w: negative public unit price",
			ErrInvalidPricingInput,
		)
	}
	ratio, err := markupRat(markup)
	if err != nil {
		return 0, err
	}
	value := new(big.Rat).Mul(
		new(big.Rat).SetInt64(unitPriceCents),
		ratio,
	)
	rounded := ceilRat(value)
	if rounded.Cmp(maxInt64Big) > 0 {
		return 0, fmt.Errorf(
			"%w: public unit price",
			ErrAmountOverflow,
		)
	}
	return rounded.Int64(), nil
}

func (c *Calculator) CalculateMixed(
	input MixedCalculationInput,
) (CalculationResult, error) {
	if c == nil {
		return CalculationResult{}, fmt.Errorf(
			"%w: nil calculator",
			ErrInvalidPricingInput,
		)
	}
	if err := ValidateUsage(input.ActualUsage); err != nil {
		return CalculationResult{}, err
	}
	if err := ValidateUsage(input.EstimatedUsage); err != nil {
		return CalculationResult{}, err
	}
	if err := ValidateRoutePrice(input.Price); err != nil {
		return CalculationResult{}, err
	}
	estimatedUsage, err := applyTokenSafetyFactor(
		input.EstimatedUsage,
		c.tokenSafetyFactor,
	)
	if err != nil {
		return CalculationResult{}, err
	}
	actualRaw, err := rawCostBasis(
		input.ActualUsage,
		input.Price,
		input.ActualInputMode,
		input.Modalities,
	)
	if err != nil {
		return CalculationResult{}, err
	}
	estimatedRaw, err := rawCostBasis(
		estimatedUsage,
		input.Price,
		input.EstimatedInputMode,
		input.Modalities,
	)
	if err != nil {
		return CalculationResult{}, err
	}
	usage, err := addUsage(input.ActualUsage, estimatedUsage)
	if err != nil {
		return CalculationResult{}, err
	}
	markup, err := markupRat(input.Price.MarkupCoefficient)
	if err != nil {
		return CalculationResult{}, err
	}
	totalRaw := new(big.Rat).SetInt64(actualRaw)
	totalRaw.Add(
		totalRaw,
		new(big.Rat).Mul(
			new(big.Rat).SetInt64(estimatedRaw),
			c.costSafetyFactor,
		),
	)
	upstream, err := ceilRawRatCents(totalRaw)
	if err != nil {
		return CalculationResult{}, err
	}
	client, err := ceilRawRatCents(
		new(big.Rat).Mul(totalRaw, markup),
	)
	if err != nil {
		return CalculationResult{}, err
	}
	return CalculationResult{
		Usage:             usage,
		UpstreamCostCents: upstream,
		ClientAmountCents: client,
		Currency:          input.Price.Currency,
		Estimated:         true,
	}, nil
}

func (c *Calculator) calculate(usage domain.TokenUsage, price domain.RoutePrice, mode InputPricingMode, modalities InputModalities, estimated bool) (CalculationResult, error) {
	if err := ValidateUsage(usage); err != nil {
		return CalculationResult{}, err
	}
	if err := ValidateRoutePrice(price); err != nil {
		return CalculationResult{}, err
	}
	raw, err := rawCostBasis(usage, price, mode, modalities)
	if err != nil {
		return CalculationResult{}, err
	}
	markup, err := markupRat(price.MarkupCoefficient)
	if err != nil {
		return CalculationResult{}, err
	}
	costFactor := big.NewRat(1, 1)
	if estimated {
		costFactor = c.costSafetyFactor
	}
	upstream, err := ceilRawCents(raw, costFactor)
	if err != nil {
		return CalculationResult{}, err
	}
	clientFactor := new(big.Rat).Mul(costFactor, markup)
	client, err := ceilRawCents(raw, clientFactor)
	if err != nil {
		return CalculationResult{}, err
	}
	return CalculationResult{Usage: usage, UpstreamCostCents: upstream, ClientAmountCents: client, Currency: price.Currency, Estimated: estimated}, nil
}

func ValidateRoutePrice(price domain.RoutePrice) error {
	if price.RouteID == "" {
		return fmt.Errorf("%w: route id is empty", ErrInvalidPricingInput)
	}
	if price.Currency != "RUB" {
		return fmt.Errorf("%w: %q", ErrUnsupportedCurrency, price.Currency)
	}
	fields := []struct {
		name  string
		value int64
	}{
		{"input price", price.InputPricePer1MTokensCents},
		{"cached input price", price.CachedInputPricePer1MTokensCents},
		{"output price", price.OutputPricePer1MTokensCents},
		{"reasoning price", price.ReasoningOutputPricePer1MTokensCents},
		{"image input price", price.ImageInputPricePer1MTokensCents},
		{"audio input price", price.AudioInputPricePer1MTokensCents},
		{"audio output price", price.AudioOutputPricePer1MTokensCents},
		{"file input price", price.FileInputPricePer1MTokensCents},
		{"video input price", price.VideoInputPricePer1MTokensCents},
		{"image generation price", price.ImageGenerationPricePerUnitCents},
	}
	for _, field := range fields {
		if field.value < 0 {
			return fmt.Errorf("%w: %s is negative", ErrInvalidPricingInput, field.name)
		}
	}
	if math.IsNaN(price.MarkupCoefficient) || math.IsInf(price.MarkupCoefficient, 0) || price.MarkupCoefficient <= 0 {
		return fmt.Errorf("%w: invalid markup coefficient", ErrInvalidMarkup)
	}
	return nil
}

func rawCostBasis(usage domain.TokenUsage, price domain.RoutePrice, mode InputPricingMode, modalities InputModalities) (int64, error) {
	var total int64
	addToken := func(count, unitPrice int64, name string) error {
		if count > 0 && unitPrice == 0 {
			return fmt.Errorf("%w: %s", ErrMissingPrice, name)
		}
		component, err := checkedMul(count, unitPrice)
		if err != nil {
			return err
		}
		total, err = checkedAdd(total, component)
		return err
	}
	if mode == "" {
		mode = InputPricingModeDetailed
	}
	switch mode {
	case InputPricingModeDetailed:
		if err := addToken(usage.InputTokens, price.InputPricePer1MTokensCents, "input tokens"); err != nil {
			return 0, err
		}
		if err := addToken(usage.CachedInputTokens, price.CachedInputPricePer1MTokensCents, "cached input tokens"); err != nil {
			return 0, err
		}
		if err := addToken(usage.ImageInputTokens, price.ImageInputPricePer1MTokensCents, "image input tokens"); err != nil {
			return 0, err
		}
		if err := addToken(usage.AudioInputTokens, price.AudioInputPricePer1MTokensCents, "audio input tokens"); err != nil {
			return 0, err
		}
		if err := addToken(usage.FileInputTokens, price.FileInputPricePer1MTokensCents, "file input tokens"); err != nil {
			return 0, err
		}
		if err := addToken(usage.VideoInputTokens, price.VideoInputPricePer1MTokensCents, "video input tokens"); err != nil {
			return 0, err
		}
	case InputPricingModeAggregateMax:
		if usage.CachedInputTokens != 0 || usage.ImageInputTokens != 0 || usage.AudioInputTokens != 0 || usage.FileInputTokens != 0 || usage.VideoInputTokens != 0 {
			return 0, fmt.Errorf("%w: aggregate input includes breakdown tokens", ErrInvalidUsage)
		}
		inputPrice, err := aggregateInputPrice(usage.InputTokens, price, modalities)
		if err != nil {
			return 0, err
		}
		if err := addToken(usage.InputTokens, inputPrice, "aggregate input tokens"); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("%w: input pricing mode", ErrInvalidPricingInput)
	}
	if err := addToken(usage.OutputTokens, price.OutputPricePer1MTokensCents, "output tokens"); err != nil {
		return 0, err
	}
	if err := addToken(usage.ReasoningTokens, price.ReasoningOutputPricePer1MTokensCents, "reasoning tokens"); err != nil {
		return 0, err
	}
	if err := addToken(usage.AudioOutputTokens, price.AudioOutputPricePer1MTokensCents, "audio output tokens"); err != nil {
		return 0, err
	}
	if usage.ImageGenerationUnits > 0 {
		if price.ImageGenerationPricePerUnitCents <= 0 || price.ImageGenerationUnitKind != domain.ImageGenerationUnitKindGeneratedImage {
			return 0, fmt.Errorf("%w: image generation units require generated_image unit price", ErrInvalidImageGenerationPricing)
		}
		unitsPrice, err := checkedMul(usage.ImageGenerationUnits, price.ImageGenerationPricePerUnitCents)
		if err != nil {
			return 0, err
		}
		component, err := checkedMul(unitsPrice, microCentsPerCent)
		if err != nil {
			return 0, err
		}
		total, err = checkedAdd(total, component)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func aggregateInputPrice(inputTokens int64, price domain.RoutePrice, modalities InputModalities) (int64, error) {
	if inputTokens > 0 && price.InputPricePer1MTokensCents == 0 {
		return 0, fmt.Errorf("%w: aggregate text input tokens", ErrMissingPrice)
	}
	max := price.InputPricePer1MTokensCents
	if modalities.Image {
		if price.ImageInputPricePer1MTokensCents == 0 {
			return 0, fmt.Errorf("%w: aggregate image input tokens", ErrMissingPrice)
		}
		if price.ImageInputPricePer1MTokensCents > max {
			max = price.ImageInputPricePer1MTokensCents
		}
	}
	if modalities.Audio {
		if price.AudioInputPricePer1MTokensCents == 0 {
			return 0, fmt.Errorf("%w: aggregate audio input tokens", ErrMissingPrice)
		}
		if price.AudioInputPricePer1MTokensCents > max {
			max = price.AudioInputPricePer1MTokensCents
		}
	}
	if modalities.File {
		if price.FileInputPricePer1MTokensCents == 0 {
			return 0, fmt.Errorf("%w: aggregate file input tokens", ErrMissingPrice)
		}
		if price.FileInputPricePer1MTokensCents > max {
			max = price.FileInputPricePer1MTokensCents
		}
	}
	if modalities.Video {
		if price.VideoInputPricePer1MTokensCents == 0 {
			return 0, fmt.Errorf("%w: aggregate video input tokens", ErrMissingPrice)
		}
		if price.VideoInputPricePer1MTokensCents > max {
			max = price.VideoInputPricePer1MTokensCents
		}
	}
	return max, nil
}

func checkedMul(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("%w: negative operand", ErrAmountOverflow)
	}
	product := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	if product.Cmp(maxInt64Big) > 0 {
		return 0, fmt.Errorf("%w: multiplication", ErrAmountOverflow)
	}
	return product.Int64(), nil
}

func checkedAdd(a, b int64) (int64, error) {
	sum := new(big.Int).Add(big.NewInt(a), big.NewInt(b))
	if sum.Cmp(maxInt64Big) > 0 {
		return 0, fmt.Errorf("%w: sum", ErrAmountOverflow)
	}
	return sum.Int64(), nil
}

func ceilRawCents(raw int64, factor *big.Rat) (int64, error) {
	if raw == 0 {
		return 0, nil
	}
	r := new(big.Rat).SetInt64(raw)
	r.Mul(r, factor)
	return ceilRawRatCents(r)
}

func ceilRawRatCents(raw *big.Rat) (int64, error) {
	if raw == nil || raw.Sign() == 0 {
		return 0, nil
	}
	r := new(big.Rat).Quo(
		new(big.Rat).Set(raw),
		big.NewRat(microCentsPerCent, 1),
	)
	value := ceilRat(r)
	if value.Cmp(maxInt64Big) > 0 {
		return 0, fmt.Errorf("%w: final amount", ErrAmountOverflow)
	}
	return value.Int64(), nil
}

func ceilRat(r *big.Rat) *big.Int {
	n := new(big.Int).Set(r.Num())
	d := new(big.Int).Set(r.Denom())
	q, rem := new(big.Int).QuoRem(n, d, new(big.Int))
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

func positiveFiniteRat(value float64, name string) (*big.Rat, error) {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPricingInput, name)
	}
	return floatToRat(value)
}

func markupRat(value float64) (*big.Rat, error) {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("%w: markup coefficient", ErrInvalidMarkup)
	}
	return floatToRat(value)
}

func floatToRat(value float64) (*big.Rat, error) {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	rat, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("%w: decimal coefficient", ErrInvalidPricingInput)
	}
	return rat, nil
}

func addUsage(
	left domain.TokenUsage,
	right domain.TokenUsage,
) (domain.TokenUsage, error) {
	out := left
	var err error
	if out.InputTokens, err = checkedAdd(left.InputTokens, right.InputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.CachedInputTokens, err = checkedAdd(left.CachedInputTokens, right.CachedInputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.OutputTokens, err = checkedAdd(left.OutputTokens, right.OutputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.ReasoningTokens, err = checkedAdd(left.ReasoningTokens, right.ReasoningTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.ImageInputTokens, err = checkedAdd(left.ImageInputTokens, right.ImageInputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.AudioInputTokens, err = checkedAdd(left.AudioInputTokens, right.AudioInputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.AudioOutputTokens, err = checkedAdd(left.AudioOutputTokens, right.AudioOutputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.FileInputTokens, err = checkedAdd(left.FileInputTokens, right.FileInputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.VideoInputTokens, err = checkedAdd(left.VideoInputTokens, right.VideoInputTokens); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.ImageGenerationUnits, err = checkedAdd(left.ImageGenerationUnits, right.ImageGenerationUnits); err != nil {
		return domain.TokenUsage{}, err
	}
	return out, nil
}

func applyTokenSafetyFactor(usage domain.TokenUsage, factor *big.Rat) (domain.TokenUsage, error) {
	var err error
	out := usage
	if out.InputTokens, err = ceilMulRat(usage.InputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.CachedInputTokens, err = ceilMulRat(usage.CachedInputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.OutputTokens, err = ceilMulRat(usage.OutputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.ReasoningTokens, err = ceilMulRat(usage.ReasoningTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.ImageInputTokens, err = ceilMulRat(usage.ImageInputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.AudioInputTokens, err = ceilMulRat(usage.AudioInputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.AudioOutputTokens, err = ceilMulRat(usage.AudioOutputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.FileInputTokens, err = ceilMulRat(usage.FileInputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	if out.VideoInputTokens, err = ceilMulRat(usage.VideoInputTokens, factor); err != nil {
		return domain.TokenUsage{}, err
	}
	return out, nil
}

func ceilMulRat(value int64, factor *big.Rat) (int64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%w: token safety factor negative usage", ErrInvalidUsage)
	}
	if value == 0 {
		return 0, nil
	}
	r := new(big.Rat).Mul(new(big.Rat).SetInt64(value), factor)
	ceil := ceilRat(r)
	if ceil.Cmp(maxInt64Big) > 0 {
		return 0, fmt.Errorf("%w: token estimate", ErrAmountOverflow)
	}
	return ceil.Int64(), nil
}
