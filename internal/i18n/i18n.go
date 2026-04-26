// Package i18n provides simple internationalization support for 9router.
// It loads translation catalogs from embedded YAML/JSON files and provides
// a T() function to translate message keys with optional parameter substitution.
package i18n

import (
	"fmt"
	"strings"
	"sync"
)

// Locale identifies a language/region, e.g. "en", "id", "zh-CN".
type Locale string

const (
	LocaleEnglish    Locale = "en"
	LocaleIndonesian Locale = "id"
	LocaleChinese    Locale = "zh"
	LocaleJapanese   Locale = "ja"
)

// Catalog holds all translations for a single locale.
type Catalog map[string]string

// Bundle holds all locale catalogs and provides translation functions.
type Bundle struct {
	mu       sync.RWMutex
	catalogs map[Locale]Catalog
	fallback Locale
}

// NewBundle creates an empty bundle with English as fallback.
func NewBundle() *Bundle {
	b := &Bundle{
		catalogs: make(map[Locale]Catalog),
		fallback: LocaleEnglish,
	}
	b.loadDefaults()
	return b
}

// RegisterCatalog adds or replaces the catalog for a locale.
func (b *Bundle) RegisterCatalog(locale Locale, catalog Catalog) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.catalogs[locale] = catalog
}

// T translates a message key for the given locale. If the key is not found in
// the requested locale, it falls back to the fallback locale (English).
// Named parameters in the form {name} are substituted from params.
func (b *Bundle) T(locale Locale, key string, params ...map[string]string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	msg := b.lookup(locale, key)
	if msg == "" {
		msg = b.lookup(b.fallback, key)
	}
	if msg == "" {
		return key // Return the key itself as last resort
	}

	if len(params) > 0 && params[0] != nil {
		for k, v := range params[0] {
			msg = strings.ReplaceAll(msg, "{"+k+"}", v)
		}
	}

	return msg
}

// Sprintf works like T but allows printf-style formatting after translation.
func (b *Bundle) Sprintf(locale Locale, key string, args ...interface{}) string {
	msg := b.T(locale, key)
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

func (b *Bundle) lookup(locale Locale, key string) string {
	cat, ok := b.catalogs[locale]
	if !ok {
		return ""
	}
	return cat[key]
}

// loadDefaults loads built-in English and Indonesian catalogs.
func (b *Bundle) loadDefaults() {
	b.catalogs[LocaleEnglish] = Catalog{
		"error.not_found":          "Not found",
		"error.bad_request":        "Bad request",
		"error.unauthorized":       "Unauthorized",
		"error.internal":           "Internal server error",
		"error.provider_failed":    "Provider {provider} failed: {error}",
		"error.all_providers_down": "All providers are currently unavailable",
		"error.rate_limited":       "Rate limit exceeded, please try again later",
		"error.invalid_model":      "Unknown model: {model}",
		"success.request_ok":       "Request completed successfully",
		"info.starting":            "Starting 9router-go {version}",
		"info.config_loaded":       "Configuration loaded from {path}",
		"info.provider_active":     "Provider {name} is active",
		"info.fallback_triggered":  "Falling back from {from} to {to}",
		"cmd.setup.title":          "9router auto-configuration",
		"cmd.update.checking":      "Checking for updates...",
		"cmd.update.up_to_date":    "Already up to date ({version})",
		"cmd.update.updated":       "Updated to {version} — please restart 9router",
	}

	b.catalogs[LocaleIndonesian] = Catalog{
		"error.not_found":          "Tidak ditemukan",
		"error.bad_request":        "Permintaan tidak valid",
		"error.unauthorized":       "Tidak terotorisasi",
		"error.internal":           "Kesalahan server internal",
		"error.provider_failed":    "Provider {provider} gagal: {error}",
		"error.all_providers_down": "Semua provider sedang tidak tersedia",
		"error.rate_limited":       "Batas kecepatan terlampaui, silakan coba lagi nanti",
		"error.invalid_model":      "Model tidak dikenal: {model}",
		"success.request_ok":       "Permintaan berhasil diselesaikan",
		"info.starting":            "Menjalankan 9router-go {version}",
		"info.config_loaded":       "Konfigurasi dimuat dari {path}",
		"info.provider_active":     "Provider {name} aktif",
		"info.fallback_triggered":  "Beralih dari {from} ke {to}",
		"cmd.setup.title":          "Konfigurasi otomatis 9router",
		"cmd.update.checking":      "Memeriksa pembaruan...",
		"cmd.update.up_to_date":    "Sudah versi terbaru ({version})",
		"cmd.update.updated":       "Diperbarui ke {version} — silakan restart 9router",
	}

	b.catalogs[LocaleChinese] = Catalog{
		"error.not_found":          "未找到",
		"error.bad_request":        "请求无效",
		"error.unauthorized":       "未授权",
		"error.internal":           "内部服务器错误",
		"error.provider_failed":    "提供商 {provider} 失败: {error}",
		"error.all_providers_down": "所有提供商当前不可用",
		"error.rate_limited":       "超过速率限制，请稍后再试",
		"error.invalid_model":      "未知模型: {model}",
		"success.request_ok":       "请求已成功完成",
		"info.starting":            "正在启动 9router-go {version}",
		"info.config_loaded":       "从 {path} 加载配置",
		"info.provider_active":     "提供商 {name} 已激活",
		"info.fallback_triggered":  "从 {from} 回退到 {to}",
	}

	b.catalogs[LocaleJapanese] = Catalog{
		"error.not_found":          "見つかりません",
		"error.bad_request":        "不正なリクエスト",
		"error.unauthorized":       "認証されていません",
		"error.internal":           "内部サーバーエラー",
		"error.provider_failed":    "プロバイダー {provider} に失敗しました: {error}",
		"error.all_providers_down": "すべてのプロバイダーが現在利用できません",
		"error.rate_limited":       "レート制限を超えました。後でもう一度お試しください",
		"error.invalid_model":      "不明なモデル: {model}",
		"success.request_ok":       "リクエストが正常に完了しました",
		"info.starting":            "9router-go {version} を起動中",
		"info.config_loaded":       "{path} から設定を読み込みました",
		"info.provider_active":     "プロバイダー {name} がアクティブです",
		"info.fallback_triggered":  "{from} から {to} にフォールバック",
	}
}

// Default is the package-level bundle using English as fallback.
var Default = NewBundle()

// T is a convenience function using the default bundle.
func T(locale Locale, key string, params ...map[string]string) string {
	return Default.T(locale, key, params...)
}
