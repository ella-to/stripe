package stripe

import (
	"context"
	"testing"
)

func TestPlatformFeeCompute(t *testing.T) {
	cases := []struct {
		name   string
		fee    PlatformFee
		amount int64
		want   int64
	}{
		{"flat $3 on $10", PlatformFee{Fixed: Dollars(3)}, Dollars(10), 300},
		{"percent only 10% of $50", PlatformFee{Percent: 10}, Dollars(50), 500},
		{"percent + fixed 2.9% + $0.30 on $20", PlatformFee{Percent: 2.9, Fixed: 30}, Dollars(20), 88},
		{"zero fee", PlatformFee{}, Dollars(99), 0},
		{"rounding 2.9% of $10.07", PlatformFee{Percent: 2.9}, 1007, 29},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fee.Compute(tc.amount); got != tc.want {
				t.Fatalf("Compute(%d) = %d, want %d", tc.amount, got, tc.want)
			}
		})
	}
}

func TestPlatformFeeFromMetadata(t *testing.T) {
	t.Run("none configured", func(t *testing.T) {
		_, ok, err := platformFeeFromMetadata(map[string]string{"other": "x"})
		if err != nil || ok {
			t.Fatalf("expected ok=false err=nil, got ok=%v err=%v", ok, err)
		}
	})

	t.Run("round trip", func(t *testing.T) {
		md := map[string]string{
			feePercentKey: "2.9",
			feeFixedKey:   "30",
		}
		fee, ok, err := platformFeeFromMetadata(md)
		if err != nil || !ok {
			t.Fatalf("expected ok=true err=nil, got ok=%v err=%v", ok, err)
		}
		if fee.Percent != 2.9 || fee.Fixed != 30 {
			t.Fatalf("got %+v", fee)
		}
	})

	t.Run("invalid percent", func(t *testing.T) {
		if _, _, err := platformFeeFromMetadata(map[string]string{feePercentKey: "abc"}); err == nil {
			t.Fatal("expected error for invalid percent")
		}
	})
}

func TestResolveFeeUsesCustomResolver(t *testing.T) {
	var asked string
	client := New("sk_test_x", WithFeeResolver(FeeResolverFunc(
		func(ctx context.Context, accountID string) (PlatformFee, bool, error) {
			asked = accountID
			return PlatformFee{Percent: 10}, true, nil
		},
	)))

	// No explicit fee -> the custom resolver is consulted.
	fee, err := client.resolveFee(context.Background(), "acct_db", nil)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "acct_db" {
		t.Fatalf("resolver asked for %q, want acct_db", asked)
	}
	if fee.Percent != 10 {
		t.Fatalf("got %+v, want Percent=10", fee)
	}

	// An explicit fee always wins and the resolver is not consulted.
	asked = ""
	explicit := PlatformFee{Fixed: Dollars(2)}
	fee, err = client.resolveFee(context.Background(), "acct_db", &explicit)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "" {
		t.Fatalf("resolver should not be consulted when fee is explicit, but got %q", asked)
	}
	if fee.Fixed != 200 {
		t.Fatalf("got %+v, want Fixed=200", fee)
	}
}
