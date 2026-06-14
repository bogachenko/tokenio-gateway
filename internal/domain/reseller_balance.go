package domain

// CanReserveResellerBalance evaluates the canonical reseller availability rule
// without mutating the reseller snapshot.
func CanReserveResellerBalance(
	reseller Reseller,
	amountCents int64,
) bool {
	if amountCents < 0 ||
		!reseller.Enabled ||
		reseller.ReservedCents < 0 ||
		reseller.MinimumBalanceCents < 0 ||
		reseller.BalanceCents < 0 {
		return false
	}
	if reseller.ReservedCents > reseller.BalanceCents {
		return false
	}

	afterReservations := reseller.BalanceCents - reseller.ReservedCents
	if reseller.MinimumBalanceCents > afterReservations {
		return false
	}

	available := afterReservations - reseller.MinimumBalanceCents
	return amountCents <= available
}
