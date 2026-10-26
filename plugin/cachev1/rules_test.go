package cachev1

import (
	"strings"
	"testing"
	"time"

	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

func TestMatchName(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"shop.cart.v1.Cart", "shop.cart.v1.Cart", true},
		{"shop.cart.v1.Cart", "shop.cart.v1.Item", false},
		{"shop.*.v1.*", "shop.cart.v1.Cart", true},
		{"shop.*.v1.*", "shop.cart.v1.Cart.Line", false},
		{"shop.**", "shop.cart.v1.Cart", true},
		{"shop.**", "store.cart.v1.Cart", false},
		{"shop.cart.**", "shop.cart", true},
	} {
		if got := matchName(strings.Split(c.pattern, "."), strings.Split(c.name, ".")); got != c.want {
			t.Errorf("matchName(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestRuleOptions(t *testing.T) {
	off := false
	o, err := Rule{Match: "a.**", Strategy: "indexed", TTL: "5m", NegativeTTL: "30s", List: &ListRule{Enabled: true, TTL: "1m"}}.Options()
	if err != nil {
		t.Fatal(err)
	}
	if !o.GetEnabled() || o.GetStrategy() != cachepbv1.Strategy_STRATEGY_INDEXED || o.GetTtl().AsDuration() != 5*time.Minute ||
		o.GetNegativeTtl().AsDuration() != 30*time.Second || o.GetStale() != nil || !o.GetList().GetEnabled() || o.GetList().GetTtl().AsDuration() != time.Minute {
		t.Errorf("options %v", o)
	}
	if o, _ := (Rule{Match: "a.**", Enabled: &off}).Options(); o.GetEnabled() {
		t.Error("enabled: false is not honoured")
	}
	for _, r := range []Rule{{}, {Match: "a", Strategy: "lru"}, {Match: "a", TTL: "soon"}, {Match: "a", List: &ListRule{TTL: "x"}}} {
		if _, err := r.Options(); err == nil {
			t.Errorf("%+v: want an error", r)
		}
	}
}
