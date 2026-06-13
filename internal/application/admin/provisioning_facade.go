package admin

import "context"

func (s *Service) ListAPIKeyProvisionings(
	ctx context.Context,
	input APIKeyProvisioningListInput,
) (ListResult[APIKeyProvisioningView], error) {
	if s == nil || s.provisioning == nil {
		return ListResult[APIKeyProvisioningView]{}, ErrInternal
	}
	return s.provisioning.ListAPIKeyProvisionings(ctx, input)
}
