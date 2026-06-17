package openaicompat

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type fixedClassifier struct{}

func (fixedClassifier) Classify(int, map[string][]string, []byte, bool) forwarding.Classification {
	return forwarding.Classification{}
}

func TestClassifierRegistryResolvesByProviderType(t *testing.T) {
	registry, err := NewClassifierRegistry(
		ClassifierRegistration{ProviderType: domain.ProviderOpenAI, Classifier: fixedClassifier{}},
		ClassifierRegistration{ProviderType: domain.ProviderOpenRouter, Classifier: fixedClassifier{}},
	)
	if err != nil {
		t.Fatalf("NewClassifierRegistry: %v", err)
	}
	if _, err := registry.Resolve(domain.ProviderOpenAI); err != nil {
		t.Fatalf("Resolve OpenAI: %v", err)
	}
	if _, err := registry.Resolve(domain.ProviderOpenRouter); err != nil {
		t.Fatalf("Resolve OpenRouter: %v", err)
	}
}

func TestClassifierRegistryRejectsInvalidRegistration(t *testing.T) {
	tests := []struct {
		name          string
		registrations []ClassifierRegistration
		want          error
	}{
		{name: "empty registry", want: ErrClassifierRegistrationInvalid},
		{name: "empty provider type", registrations: []ClassifierRegistration{{Classifier: fixedClassifier{}}}, want: ErrClassifierRegistrationInvalid},
		{name: "nil classifier", registrations: []ClassifierRegistration{{ProviderType: domain.ProviderOpenAI}}, want: ErrClassifierRegistrationInvalid},
		{
			name: "duplicate provider",
			registrations: []ClassifierRegistration{
				{ProviderType: domain.ProviderOpenAI, Classifier: fixedClassifier{}},
				{ProviderType: domain.ProviderOpenAI, Classifier: fixedClassifier{}},
			},
			want: ErrClassifierAlreadyRegistered,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewClassifierRegistry(test.registrations...)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestClassifierRegistryRejectsUnknownProvider(t *testing.T) {
	registry, err := NewClassifierRegistry(
		ClassifierRegistration{ProviderType: domain.ProviderOpenAI, Classifier: fixedClassifier{}},
	)
	if err != nil {
		t.Fatalf("NewClassifierRegistry: %v", err)
	}
	_, err = registry.Resolve(domain.ProviderHydra)
	if !errors.Is(err, ErrClassifierNotRegistered) {
		t.Fatalf("error=%v", err)
	}
}
