package openaicompat

import (
	"errors"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var (
	ErrClassifierRegistrationInvalid = errors.New("invalid error classifier registration")
	ErrClassifierAlreadyRegistered   = errors.New("error classifier already registered")
	ErrClassifierNotRegistered       = errors.New("error classifier not registered")
)

type ClassifierRegistration struct {
	ProviderType domain.ProviderType
	Classifier   ErrorClassifier
}

type ErrorClassifierResolver interface {
	Resolve(domain.ProviderType) (ErrorClassifier, error)
}

type ClassifierRegistry struct {
	byProvider map[domain.ProviderType]ErrorClassifier
}

func NewClassifierRegistry(registrations ...ClassifierRegistration) (*ClassifierRegistry, error) {
	if len(registrations) == 0 {
		return nil, ErrClassifierRegistrationInvalid
	}
	byProvider := make(map[domain.ProviderType]ErrorClassifier, len(registrations))
	for _, registration := range registrations {
		if registration.ProviderType == "" || registration.Classifier == nil {
			return nil, ErrClassifierRegistrationInvalid
		}
		if _, exists := byProvider[registration.ProviderType]; exists {
			return nil, fmt.Errorf("%w: provider_type=%s", ErrClassifierAlreadyRegistered, registration.ProviderType)
		}
		byProvider[registration.ProviderType] = registration.Classifier
	}
	return &ClassifierRegistry{byProvider: byProvider}, nil
}

func (registry *ClassifierRegistry) Resolve(providerType domain.ProviderType) (ErrorClassifier, error) {
	if registry == nil || registry.byProvider == nil || providerType == "" {
		return nil, ErrClassifierNotRegistered
	}
	classifier, found := registry.byProvider[providerType]
	if !found || classifier == nil {
		return nil, fmt.Errorf("%w: provider_type=%s", ErrClassifierNotRegistered, providerType)
	}
	return classifier, nil
}

var _ ErrorClassifierResolver = (*ClassifierRegistry)(nil)
