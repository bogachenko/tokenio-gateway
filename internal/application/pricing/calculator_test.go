package pricing

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func testPrice() domain.RoutePrice {
	return domain.RoutePrice{RouteID: "route-1", Currency: "RUB", InputPricePer1MTokensCents: 100, CachedInputPricePer1MTokensCents: 10, OutputPricePer1MTokensCents: 200, ReasoningOutputPricePer1MTokensCents: 300, ImageInputPricePer1MTokensCents: 400, AudioInputPricePer1MTokensCents: 500, AudioOutputPricePer1MTokensCents: 600, FileInputPricePer1MTokensCents: 700, VideoInputPricePer1MTokensCents: 800, ImageGenerationPricePerUnitCents: 9, ImageGenerationUnitKind: domain.ImageGenerationUnitKindGeneratedImage, MarkupCoefficient: 1.5}
}

func newTestCalculator(t *testing.T) *Calculator {
	t.Helper()
	c, err := NewCalculator(1.25, 1.10)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func actual(t *testing.T, usage domain.TokenUsage, price domain.RoutePrice) CalculationResult {
	t.Helper()
	got, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{Usage: usage, Price: price, InputMode: InputPricingModeDetailed})
	if err != nil {
		t.Fatalf("CalculateActual error: %v", err)
	}
	return got
}

func TestCalculateMixedAppliesSafetyOnlyToEstimate(
	t *testing.T,
) {
	calculator := newTestCalculator(t)
	result, err := calculator.CalculateMixed(
		MixedCalculationInput{
			ActualUsage: domain.TokenUsage{
				InputTokens: 1_000_000,
			},
			EstimatedUsage: domain.TokenUsage{
				OutputTokens: 4_000,
			},
			Price:              testPrice(),
			ActualInputMode:    InputPricingModeAggregateMax,
			EstimatedInputMode: InputPricingModeDetailed,
		},
	)
	if err != nil {
		t.Fatalf("CalculateMixed: %v", err)
	}
	if result.Usage.InputTokens != 1_000_000 ||
		result.Usage.OutputTokens != 5_000 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if result.UpstreamCostCents != 102 {
		t.Fatalf(
			"upstream cents = %d, want 102",
			result.UpstreamCostCents,
		)
	}
	if result.ClientAmountCents != 152 {
		t.Fatalf(
			"client cents = %d, want 152",
			result.ClientAmountCents,
		)
	}
	if !result.Estimated {
		t.Fatal("mixed result must be marked estimated")
	}
}

func TestCalculatePublicUnitPriceCentsUsesCanonicalMarkup(
	t *testing.T,
) {
	calculator := newTestCalculator(t)

	value, err := calculator.CalculatePublicUnitPriceCents(
		101,
		1.5,
	)
	if err != nil || value != 152 {
		t.Fatalf("value=%d error=%v", value, err)
	}
	decimalValue, err :=
		calculator.CalculatePublicUnitPriceCents(
			100,
			1.1,
		)
	if err != nil || decimalValue != 110 {
		t.Fatalf(
			"decimal value=%d error=%v",
			decimalValue,
			err,
		)
	}
	_, err = calculator.CalculatePublicUnitPriceCents(
		math.MaxInt64,
		2,
	)
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestActualDetailedPricingAllCategories(t *testing.T) {
	price := testPrice()
	cases := []struct {
		name             string
		usage            domain.TokenUsage
		upstream, client int64
	}{
		{"text input", domain.TokenUsage{InputTokens: 10_000}, 1, 2}, {"cached input", domain.TokenUsage{CachedInputTokens: 100_000}, 1, 2},
		{"output", domain.TokenUsage{OutputTokens: 5_000}, 1, 2}, {"reasoning", domain.TokenUsage{ReasoningTokens: 4_000}, 2, 2},
		{"image input", domain.TokenUsage{ImageInputTokens: 3_000}, 2, 2}, {"audio input", domain.TokenUsage{AudioInputTokens: 2_000}, 1, 2},
		{"audio output", domain.TokenUsage{AudioOutputTokens: 2_000}, 2, 2}, {"file input", domain.TokenUsage{FileInputTokens: 2_000}, 2, 3},
		{"video input", domain.TokenUsage{VideoInputTokens: 2_000}, 2, 3}, {"image generation", domain.TokenUsage{ImageGenerationUnits: 2}, 18, 27},
		{"zero", domain.TokenUsage{}, 0, 0},
		{"all tokens", domain.TokenUsage{InputTokens: 10_000, CachedInputTokens: 100_000, OutputTokens: 5_000, ReasoningTokens: 4_000, ImageInputTokens: 3_000, AudioInputTokens: 2_000, AudioOutputTokens: 2_000, FileInputTokens: 2_000, VideoInputTokens: 2_000}, 11, 16},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := actual(t, tt.usage, price)
			if got.UpstreamCostCents != tt.upstream || got.ClientAmountCents != tt.client {
				t.Fatalf("got upstream/client %d/%d want %d/%d", got.UpstreamCostCents, got.ClientAmountCents, tt.upstream, tt.client)
			}
		})
	}
}

func TestMissingPricesAndImageGenerationValidation(t *testing.T) {
	base := testPrice()
	categoryCases := []struct {
		name  string
		usage domain.TokenUsage
		zero  func(*domain.RoutePrice)
	}{
		{"input", domain.TokenUsage{InputTokens: 1}, func(p *domain.RoutePrice) { p.InputPricePer1MTokensCents = 0 }}, {"cached", domain.TokenUsage{CachedInputTokens: 1}, func(p *domain.RoutePrice) { p.CachedInputPricePer1MTokensCents = 0 }},
		{"output", domain.TokenUsage{OutputTokens: 1}, func(p *domain.RoutePrice) { p.OutputPricePer1MTokensCents = 0 }}, {"reasoning", domain.TokenUsage{ReasoningTokens: 1}, func(p *domain.RoutePrice) { p.ReasoningOutputPricePer1MTokensCents = 0 }},
		{"image input", domain.TokenUsage{ImageInputTokens: 1}, func(p *domain.RoutePrice) { p.ImageInputPricePer1MTokensCents = 0 }}, {"audio input", domain.TokenUsage{AudioInputTokens: 1}, func(p *domain.RoutePrice) { p.AudioInputPricePer1MTokensCents = 0 }},
		{"audio output", domain.TokenUsage{AudioOutputTokens: 1}, func(p *domain.RoutePrice) { p.AudioOutputPricePer1MTokensCents = 0 }}, {"file input", domain.TokenUsage{FileInputTokens: 1}, func(p *domain.RoutePrice) { p.FileInputPricePer1MTokensCents = 0 }},
		{"video input", domain.TokenUsage{VideoInputTokens: 1}, func(p *domain.RoutePrice) { p.VideoInputPricePer1MTokensCents = 0 }},
	}
	for _, tt := range categoryCases {
		t.Run(tt.name, func(t *testing.T) {
			p := base
			tt.zero(&p)
			_, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{Usage: tt.usage, Price: p, InputMode: InputPricingModeDetailed})
			if !errors.Is(err, ErrMissingPrice) {
				t.Fatalf("err = %v, want ErrMissingPrice", err)
			}
			got := actual(t, domain.TokenUsage{}, p)
			if got.UpstreamCostCents != 0 || got.ClientAmountCents != 0 {
				t.Fatalf("zero usage with zero category price should be allowed")
			}
		})
	}
	p := base
	p.ImageGenerationPricePerUnitCents = 0
	_, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{ImageGenerationUnits: 1}, Price: p})
	if !errors.Is(err, ErrInvalidImageGenerationPricing) {
		t.Fatalf("err = %v", err)
	}
	p = base
	p.ImageGenerationUnitKind = domain.ImageGenerationUnitKindNone
	_, err = newTestCalculator(t).CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{ImageGenerationUnits: 1}, Price: p})
	if !errors.Is(err, ErrInvalidImageGenerationPricing) {
		t.Fatalf("err = %v", err)
	}
	_ = actual(t, domain.TokenUsage{ImageGenerationUnits: 1}, base)
}

func TestMarkupValidationAndSingleRounding(t *testing.T) {
	p := testPrice()
	p.InputPricePer1MTokensCents = 333_333
	p.OutputPricePer1MTokensCents = 333_333
	p.MarkupCoefficient = 1
	got := actual(t, domain.TokenUsage{InputTokens: 1, OutputTokens: 1}, p)
	if got.UpstreamCostCents != 1 || got.ClientAmountCents != 1 {
		t.Fatalf("markup 1 got %d/%d", got.UpstreamCostCents, got.ClientAmountCents)
	}
	p.MarkupCoefficient = 2
	got = actual(t, domain.TokenUsage{InputTokens: 1, OutputTokens: 1}, p)
	if got.UpstreamCostCents != 1 || got.ClientAmountCents != 2 {
		t.Fatalf("markup 2 got %d/%d", got.UpstreamCostCents, got.ClientAmountCents)
	}
	p.MarkupCoefficient = 1.5
	got = actual(t, domain.TokenUsage{InputTokens: 1, OutputTokens: 1}, p)
	if got.UpstreamCostCents != 1 || got.ClientAmountCents != 1 {
		t.Fatalf("fractional markup from raw basis got %d/%d", got.UpstreamCostCents, got.ClientAmountCents)
	}
	p.ImageGenerationPricePerUnitCents = 1
	got = actual(t, domain.TokenUsage{InputTokens: 1, OutputTokens: 1, ImageGenerationUnits: 1}, p)
	if got.UpstreamCostCents != 2 || got.ClientAmountCents != 3 {
		t.Fatalf("image unit raw basis got %d/%d", got.UpstreamCostCents, got.ClientAmountCents)
	}
	for _, bad := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		p := testPrice()
		p.MarkupCoefficient = bad
		_, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{InputTokens: 1}, Price: p})
		if !errors.Is(err, ErrInvalidMarkup) {
			t.Fatalf("bad markup %v err %v", bad, err)
		}
	}
}

func TestAggregateMaxMode(t *testing.T) {
	p := testPrice()
	p.MarkupCoefficient = 1
	p.InputPricePer1MTokensCents = 100
	p.ImageInputPricePer1MTokensCents = 200
	p.AudioInputPricePer1MTokensCents = 300
	p.FileInputPricePer1MTokensCents = 400
	p.VideoInputPricePer1MTokensCents = 500
	cases := []struct {
		name string
		m    InputModalities
		want int64
	}{{"text", InputModalities{}, 1}, {"image", InputModalities{Image: true}, 2}, {"audio", InputModalities{Audio: true}, 3}, {"file", InputModalities{File: true}, 4}, {"video", InputModalities{Video: true}, 5}, {"audio file", InputModalities{Audio: true, File: true}, 4}, {"absent expensive ignored", InputModalities{Audio: true}, 3}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{InputTokens: 10_000}, Price: p, InputMode: InputPricingModeAggregateMax, Modalities: tt.m})
			if err != nil {
				t.Fatal(err)
			}
			if got.UpstreamCostCents != tt.want {
				t.Fatalf("got %d want %d", got.UpstreamCostCents, tt.want)
			}
		})
	}
	for _, usage := range []domain.TokenUsage{{InputTokens: 1, ImageInputTokens: 1}, {InputTokens: 1, CachedInputTokens: 1}} {
		_, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{Usage: usage, Price: p, InputMode: InputPricingModeAggregateMax})
		if !errors.Is(err, ErrInvalidUsage) {
			t.Fatalf("err = %v, want ErrInvalidUsage", err)
		}
	}
	got := actual(t, domain.TokenUsage{InputTokens: 10_000, ImageInputTokens: 10_000}, p)
	if got.UpstreamCostCents != 3 {
		t.Fatalf("detailed mode got %d", got.UpstreamCostCents)
	}
}

func TestAggregateMaxModeRequiresPresentModalityPrices(t *testing.T) {
	p := testPrice()
	p.MarkupCoefficient = 1
	p.InputPricePer1MTokensCents = 100
	p.ImageInputPricePer1MTokensCents = 200
	p.AudioInputPricePer1MTokensCents = 300
	p.FileInputPricePer1MTokensCents = 400
	p.VideoInputPricePer1MTokensCents = 500

	cases := []struct {
		name     string
		mutate   func(*domain.RoutePrice)
		modality InputModalities
	}{
		{"text input", func(price *domain.RoutePrice) { price.InputPricePer1MTokensCents = 0 }, InputModalities{}},
		{"image present", func(price *domain.RoutePrice) { price.ImageInputPricePer1MTokensCents = 0 }, InputModalities{Image: true}},
		{"audio present", func(price *domain.RoutePrice) { price.AudioInputPricePer1MTokensCents = 0 }, InputModalities{Audio: true}},
		{"file present", func(price *domain.RoutePrice) { price.FileInputPricePer1MTokensCents = 0 }, InputModalities{File: true}},
		{"video present", func(price *domain.RoutePrice) { price.VideoInputPricePer1MTokensCents = 0 }, InputModalities{Video: true}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			price := p
			tt.mutate(&price)
			_, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{
				Usage:      domain.TokenUsage{InputTokens: 10_000},
				Price:      price,
				InputMode:  InputPricingModeAggregateMax,
				Modalities: tt.modality,
			})
			if !errors.Is(err, ErrMissingPrice) {
				t.Fatalf("err = %v, want ErrMissingPrice", err)
			}
		})
	}

	absentZeroPrice := p
	absentZeroPrice.AudioInputPricePer1MTokensCents = 0
	got, err := newTestCalculator(t).CalculateActual(ActualCalculationInput{
		Usage:      domain.TokenUsage{InputTokens: 10_000},
		Price:      absentZeroPrice,
		InputMode:  InputPricingModeAggregateMax,
		Modalities: InputModalities{File: true},
	})
	if err != nil {
		t.Fatalf("absent zero-priced modality should not be required: %v", err)
	}
	if got.UpstreamCostCents != 4 {
		t.Fatalf("got %d want 4", got.UpstreamCostCents)
	}
}

func TestSafetyFactorsAndOverflow(t *testing.T) {
	c := newTestCalculator(t)
	p := testPrice()
	p.MarkupCoefficient = 2
	usage := domain.TokenUsage{InputTokens: 4, CachedInputTokens: 4, OutputTokens: 4, ReasoningTokens: 4, ImageInputTokens: 4, AudioInputTokens: 4, AudioOutputTokens: 4, FileInputTokens: 4, VideoInputTokens: 4, ImageGenerationUnits: 4}
	original := usage
	got, err := c.CalculateEstimate(EstimateCalculationInput{Usage: usage, Price: p, InputMode: InputPricingModeDetailed})
	if err != nil {
		t.Fatal(err)
	}
	wantUsage := domain.TokenUsage{InputTokens: 5, CachedInputTokens: 5, OutputTokens: 5, ReasoningTokens: 5, ImageInputTokens: 5, AudioInputTokens: 5, AudioOutputTokens: 5, FileInputTokens: 5, VideoInputTokens: 5, ImageGenerationUnits: 4}
	if !reflect.DeepEqual(got.Usage, wantUsage) {
		t.Fatalf("usage = %+v want %+v", got.Usage, wantUsage)
	}
	if got.UpstreamCostCents != 40 || got.ClientAmountCents != 80 {
		t.Fatalf("estimate amounts got %d/%d", got.UpstreamCostCents, got.ClientAmountCents)
	}
	if !reflect.DeepEqual(usage, original) {
		t.Fatalf("input usage mutated")
	}
	actualGot, err := c.CalculateActual(ActualCalculationInput{Usage: usage, Price: p, InputMode: InputPricingModeDetailed})
	if err != nil {
		t.Fatal(err)
	}
	if actualGot.Usage.InputTokens != 4 {
		t.Fatalf("actual applied factor")
	}
	for _, f := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NewCalculator(f, 1); !errors.Is(err, ErrInvalidPricingInput) {
			t.Fatalf("token factor %v err %v", f, err)
		}
		if _, err := NewCalculator(1, f); !errors.Is(err, ErrInvalidPricingInput) {
			t.Fatalf("cost factor %v err %v", f, err)
		}
	}
	overflowPrice := testPrice()
	overflowPrice.InputPricePer1MTokensCents = math.MaxInt64
	_, err = c.CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{InputTokens: 2}, Price: overflowPrice})
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("mul overflow err %v", err)
	}
	sumPrice := testPrice()
	sumPrice.InputPricePer1MTokensCents = math.MaxInt64
	sumPrice.OutputPricePer1MTokensCents = 1
	_, err = c.CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{InputTokens: 1, OutputTokens: 1}, Price: sumPrice})
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("sum overflow err %v", err)
	}
	bigMarkup := testPrice()
	bigMarkup.MarkupCoefficient = 1e18
	_, err = c.CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{ImageGenerationUnits: 10}, Price: bigMarkup})
	if !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("final overflow err %v", err)
	}
	stable := testPrice()
	stable.MarkupCoefficient = 1.333333333
	one, _ := c.CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{InputTokens: 12345}, Price: stable})
	two, _ := c.CalculateActual(ActualCalculationInput{Usage: domain.TokenUsage{InputTokens: 12345}, Price: stable})
	if one.ClientAmountCents != two.ClientAmountCents {
		t.Fatalf("non-deterministic fractional markup")
	}
}

func TestRoutePriceValidation(t *testing.T) {
	p := testPrice()
	p.RouteID = ""
	if err := ValidateRoutePrice(p); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("empty route err %v", err)
	}
	p = testPrice()
	p.Currency = "rub"
	if err := ValidateRoutePrice(p); !errors.Is(err, ErrUnsupportedCurrency) {
		t.Fatalf("currency err %v", err)
	}
	p = testPrice()
	p.InputPricePer1MTokensCents = -1
	if err := ValidateRoutePrice(p); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("price err %v", err)
	}
}
