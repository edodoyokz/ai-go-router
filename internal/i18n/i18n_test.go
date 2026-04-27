package i18n

import "testing"

func TestT_ReturnsCorrectLocale(t *testing.T) {
	b := NewBundle()

	cases := []struct {
		locale Locale
		key    string
		want   string
	}{
		{LocaleEnglish, "error.unauthorized", "Unauthorized"},
		{LocaleIndonesian, "error.unauthorized", "Tidak terotorisasi"},
		{LocaleChinese, "error.unauthorized", "未授权"},
		{LocaleJapanese, "error.unauthorized", "認証されていません"},
		{LocaleEnglish, "error.rate_limited", "Rate limit exceeded, please try again later"},
		{LocaleIndonesian, "error.rate_limited", "Batas kecepatan terlampaui, silakan coba lagi nanti"},
	}

	for _, tc := range cases {
		got := b.T(tc.locale, tc.key)
		if got != tc.want {
			t.Errorf("T(%q, %q) = %q, want %q", tc.locale, tc.key, got, tc.want)
		}
	}
}

func TestT_FallsBackToEnglish(t *testing.T) {
	b := NewBundle()
	// zh catalog does not have cmd.setup.title
	got := b.T(LocaleChinese, "cmd.setup.title")
	want := b.T(LocaleEnglish, "cmd.setup.title")
	if got != want {
		t.Errorf("expected fallback to English %q, got %q", want, got)
	}
}

func TestT_ReturnsKeyWhenMissing(t *testing.T) {
	b := NewBundle()
	key := "nonexistent.key"
	got := b.T(LocaleEnglish, key)
	if got != key {
		t.Errorf("expected key %q returned as-is, got %q", key, got)
	}
}

func TestT_ParamSubstitution(t *testing.T) {
	b := NewBundle()
	got := b.T(LocaleEnglish, "error.provider_failed", map[string]string{
		"provider": "openai",
		"error":    "timeout",
	})
	want := "Provider openai failed: timeout"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultBundleT(t *testing.T) {
	got := T(LocaleEnglish, "error.unauthorized")
	if got != "Unauthorized" {
		t.Errorf("package-level T() got %q, want %q", got, "Unauthorized")
	}
}

func TestRegisterCatalog(t *testing.T) {
	b := NewBundle()
	b.RegisterCatalog("xx", Catalog{
		"hello": "Hallo",
	})
	got := b.T("xx", "hello")
	if got != "Hallo" {
		t.Errorf("got %q, want %q", got, "Hallo")
	}
}
