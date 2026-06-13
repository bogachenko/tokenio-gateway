package ports_test

import (
	"context"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type provisioningCryptoFake struct{}

func (provisioningCryptoFake) CreateProvisioningMaterial(
	context.Context,
	ports.APIKeyProvisioningMaterialRequest,
) (ports.APIKeyProvisioningMaterial, error) {
	return ports.APIKeyProvisioningMaterial{}, nil
}

func (provisioningCryptoFake) DecryptProvisioningMaterial(
	context.Context,
	domain.APIKeyProvisioning,
	domain.APIKeyRecord,
) (string, error) {
	return "", nil
}

var (
	_ ports.APIKeyProvisioningMaterialFactory   = provisioningCryptoFake{}
	_ ports.APIKeyProvisioningMaterialDecryptor = provisioningCryptoFake{}
)

func TestProvisioningCryptoPortsDoNotExposePlaintextFields(
	t *testing.T,
) {
	material := ports.APIKeyProvisioningMaterial{}
	if material.APIKey.KeyHash != "" ||
		material.APIKey.KeyPrefix != "" ||
		len(material.EncryptedRawKey) != 0 ||
		len(material.EncryptionNonce) != 0 {
		t.Fatal("zero provisioning material is not empty")
	}
}
