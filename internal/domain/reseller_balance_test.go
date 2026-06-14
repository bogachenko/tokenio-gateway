package domain

import "testing"

func TestCanReserveResellerBalance(t *testing.T) {
	base := Reseller{
		Enabled:             true,
		BalanceCents:        1000,
		ReservedCents:       100,
		MinimumBalanceCents: 200,
	}

	tests := []struct {
		name   string
		mutate func(Reseller) Reseller
		amount int64
		want   bool
	}{
		{
			name:   "exact available balance",
			mutate: identityReseller,
			amount: 700,
			want:   true,
		},
		{
			name:   "amount above available balance",
			mutate: identityReseller,
			amount: 701,
			want:   false,
		},
		{
			name: "disabled reseller",
			mutate: func(value Reseller) Reseller {
				value.Enabled = false
				return value
			},
			amount: 1,
			want:   false,
		},
		{
			name: "negative balance",
			mutate: func(value Reseller) Reseller {
				value.BalanceCents = -1
				return value
			},
			amount: 0,
			want:   false,
		},
		{
			name: "reserved exceeds balance",
			mutate: func(value Reseller) Reseller {
				value.ReservedCents = value.BalanceCents + 1
				return value
			},
			amount: 0,
			want:   false,
		},
		{
			name: "minimum balance exceeds remainder",
			mutate: func(value Reseller) Reseller {
				value.MinimumBalanceCents = 901
				return value
			},
			amount: 0,
			want:   false,
		},
		{
			name:   "negative amount",
			mutate: identityReseller,
			amount: -1,
			want:   false,
		},
		{
			name: "zero cost with zero available balance",
			mutate: func(value Reseller) Reseller {
				value.BalanceCents = 300
				value.ReservedCents = 100
				value.MinimumBalanceCents = 200
				return value
			},
			amount: 0,
			want:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CanReserveResellerBalance(
				test.mutate(base),
				test.amount,
			)
			if got != test.want {
				t.Fatalf("CanReserveResellerBalance = %v, want %v", got, test.want)
			}
		})
	}
}

func identityReseller(value Reseller) Reseller {
	return value
}
