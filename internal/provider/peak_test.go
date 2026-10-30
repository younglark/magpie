package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/settings"
)

// Only the vendors whose own pricing bills DeepSeek's models by the hour
// are: DeepSeek's API, Command Code's DeepSeek V4 models (not its flat
// deepseek-v4-flash-fast) and OpenCode Go's two. OpenCode Zen, a relay, a
// lookalike host and every other model keep one price.
func TestPeakOf(t *testing.T) {
	preset := func(id string) Provider {
		p, err := FromPreset(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, c := range []struct {
		name  string
		p     Provider
		model string
		want  catalog.Peak
	}{
		{"deepseek", preset("deepseek"), "deepseek-flash", catalog.PeakDeepSeek},
		{"deepseek pro", preset("deepseek"), "deepseek-v4-pro", catalog.PeakDeepSeek},
		{"commandcode", preset("commandcode"), "deepseek/deepseek-v4.1-flash", catalog.PeakWeekdays},
		{"commandcode pro", preset("commandcode"), "deepseek/deepseek-v4-pro", catalog.PeakWeekdays},
		{"commandcode flash-fast", preset("commandcode"), "deepseek/deepseek-v4-flash-fast", ""},
		{"commandcode other", preset("commandcode"), "moonshotai/kimi-k3", ""},
		{"commandcode plan", Provider{ID: CommandCodePlanID}, "deepseek/deepseek-v4.1-flash", catalog.PeakWeekdays},
		{"opencode go", preset("opencode-go"), "deepseek-v4.1-flash", catalog.PeakWeekdays},
		{"opencode go other", preset("opencode-go"), "kimi-k3", ""},
		{"opencode zen", preset("opencode-zen"), "deepseek-v4.1-flash", ""},
		{"relay", Provider{ID: "relay", Chat: "https://relay.example/v1"}, "deepseek-v4.1-flash", ""},
		{"relay named deepseek", Provider{ID: "deepseek", Preset: "deepseek", Chat: "https://relay.example/v1"}, "deepseek-flash", ""},
		{"lookalike host", Provider{ID: "x", Chat: "https://api.deepseek.com.evil.example/v1"}, "deepseek-flash", ""},
		{"removed deepseek", Provider{ID: "deepseek"}, "deepseek-flash", catalog.PeakDeepSeek},
		{"openai", preset("openai"), "gpt-6-astra", ""},
	} {
		if got := c.p.PeakOf(c.model); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.name, c.model, got, c.want)
		}
	}
}

// useDeepSeekCatalog puts a catalog in the cache that lists DeepSeek's
// deepseek-flash at its off-peak price, as models.dev's api.json does
// (its peak multiplier left out), OpenCode Zen's deepseek-v4.1-flash at
// its flat price and an OpenAI model: no network.
func useDeepSeekCatalog(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	model := func(id string, in, out float64) map[string]any {
		return map[string]any{"id": id, "cost": map[string]any{"input": in, "output": out}}
	}
	b, _ := json.Marshal(map[string]any{
		"deepseek": map[string]any{"id": "deepseek", "models": map[string]any{
			"deepseek-flash": model("deepseek-flash", 0.15, 0.6), "deepseek-v4.1-flash": model("deepseek-v4.1-flash", 0.15, 0.6)}},
		"opencode": map[string]any{"id": "opencode", "models": map[string]any{
			"deepseek-v4.1-flash": model("deepseek-v4.1-flash", 0.3, 1.2)}},
		"openai": map[string]any{"id": "openai", "models": map[string]any{
			"gpt-6-astra": model("gpt-6-astra", 2, 8)}},
	})
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
}

// A DeepSeek model's list price doubles at DeepSeek's peak hours, at the
// provider's price rate still; a price the user set holds at every hour,
// and so do OpenCode Zen's flat price and another vendor's model at the
// same times.
func TestPriceOfPeak(t *testing.T) {
	useDeepSeekCatalog(t)
	peak := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC) // a Thursday
	off := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)
	preset := func(id string) Provider {
		p, err := FromPreset(id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	at := func(s settings.Settings, p Provider, model string, tm time.Time) float64 {
		t.Helper()
		pr, ok := priceOf(s, p, true, model)
		if !ok {
			t.Fatalf("%s %s: unpriced", p.ID, model)
		}
		return pr.In(tm).Input
	}
	none := settings.Settings{}
	ds := preset("deepseek")
	if a, b := at(none, ds, "deepseek-flash", peak), at(none, ds, "deepseek-flash", off); a != 0.3 || b != 0.15 {
		t.Errorf("deepseek-flash: %v at peak, %v off-peak; want 0.3, 0.15", a, b)
	}
	half := ds
	half.PriceRate = 0.5
	if a := at(none, half, "deepseek-flash", peak); a != 0.15 {
		t.Errorf("deepseek-flash at a 0.5 rate: %v at peak, want 0.15", a)
	}
	cc := preset("commandcode") // no catalog of its own: DeepSeek's price
	if a, b := at(none, cc, "deepseek/deepseek-v4.1-flash", peak), at(none, cc, "deepseek/deepseek-v4.1-flash", off); a != 0.3 || b != 0.15 {
		t.Errorf("commandcode deepseek-v4.1-flash: %v at peak, %v off-peak", a, b)
	}
	zen := preset("opencode-zen")
	if a, b := at(none, zen, "deepseek-v4.1-flash", peak), at(none, zen, "deepseek-v4.1-flash", off); a != 0.3 || b != 0.3 {
		t.Errorf("opencode zen deepseek-v4.1-flash: %v at peak, %v off-peak; want 0.3 both", a, b)
	}
	oa := preset("openai")
	if a, b := at(none, oa, "gpt-6-astra", peak), at(none, oa, "gpt-6-astra", off); a != 2 || b != 2 {
		t.Errorf("gpt-6-astra: %v at peak, %v off-peak; want 2 both", a, b)
	}
	// the user's own price for the model, or for every model of DeepSeek
	in, out, cache := 0.2, 0.8, 0.02
	for _, key := range []string{"deepseek/deepseek-flash", "deepseek/*", AnyPriceKey("deepseek-flash")} {
		s := settings.Settings{ModelPrices: map[string]settings.ModelPrice{key: {Input: &in, Output: &out, CacheRead: &cache, CacheWrite: &cache}}}
		if a, b := at(s, ds, "deepseek-flash", peak), at(s, ds, "deepseek-flash", off); a != 0.2 || b != 0.2 {
			t.Errorf("set at %s: %v at peak, %v off-peak; want 0.2 both", key, a, b)
		}
	}
	// a provider gone since, priced at its maker's
	if pr, ok := priceOf(none, Provider{ID: "deepseek"}, false, "deepseek-flash"); !ok || pr.In(peak).Input != 0.3 {
		t.Errorf("removed deepseek: %+v %v", pr, ok)
	}
}
