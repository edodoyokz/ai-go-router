package usage

import (
	"testing"
)

func TestNewPricingRegistry(t *testing.T) {
	pr := NewPricingRegistry()
	if pr == nil {
		t.Fatal("expected non-nil pricing registry")
	}
	if pr.models == nil {
		t.Error("expected initialized models map")
	}
}

func TestPricingRegistry_Register(t *testing.T) {
	pr := NewPricingRegistry()

	pm := PricingModel{
		Provider:              "test",
		Model:                 "model-1",
		InputPricePerMillion:  10.0,
		OutputPricePerMillion: 20.0,
		Currency:              "USD",
	}

	pr.Register(pm)

	// Should be retrievable
	retrieved, found := pr.Get("test", "model-1")
	if !found {
		t.Error("expected to find registered pricing model")
	}
	if retrieved.Provider != "test" {
		t.Errorf("expected provider test, got %s", retrieved.Provider)
	}
	if retrieved.Model != "model-1" {
		t.Errorf("expected model model-1, got %s", retrieved.Model)
	}
}

func TestPricingRegistry_RegisterDefaultCurrency(t *testing.T) {
	pr := NewPricingRegistry()

	pm := PricingModel{
		Provider:              "test",
		Model:                 "model-1",
		InputPricePerMillion:  10.0,
		OutputPricePerMillion: 20.0,
		// Currency not set
	}

	pr.Register(pm)

	retrieved, _ := pr.Get("test", "model-1")
	if retrieved.Currency != "USD" {
		t.Errorf("expected default currency USD, got %s", retrieved.Currency)
	}
}

func TestPricingRegistry_Get_NotFound(t *testing.T) {
	pr := NewPricingRegistry()

	_, found := pr.Get("nonexistent", "model")
	if found {
		t.Error("expected not to find nonexistent model")
	}
}

func TestPricingModel_CalculateCost(t *testing.T) {
	pm := PricingModel{
		Provider:              "openai",
		Model:                 "gpt-4",
		InputPricePerMillion:  30.0,
		OutputPricePerMillion: 60.0,
		Currency:              "USD",
	}

	// Calculate cost for 1M input tokens and 500K output tokens
	cost := pm.CalculateCost(1_000_000, 500_000)

	// Expected: input = $30, output = $30, total = $60
	if cost.InputCost != 30.0 {
		t.Errorf("expected input cost $30.0, got $%f", cost.InputCost)
	}
	if cost.OutputCost != 30.0 {
		t.Errorf("expected output cost $30.0, got $%f", cost.OutputCost)
	}
	if cost.TotalCost != 60.0 {
		t.Errorf("expected total cost $60.0, got $%f", cost.TotalCost)
	}
	if cost.InputTokens != 1_000_000 {
		t.Errorf("expected input tokens 1000000, got %d", cost.InputTokens)
	}
	if cost.OutputTokens != 500_000 {
		t.Errorf("expected output tokens 500000, got %d", cost.OutputTokens)
	}
	if cost.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", cost.Currency)
	}
}

func TestPricingModel_CalculateCost_ZeroTokens(t *testing.T) {
	pm := PricingModel{
		Provider:              "openai",
		Model:                 "gpt-4",
		InputPricePerMillion:  30.0,
		OutputPricePerMillion: 60.0,
		Currency:              "USD",
	}

	cost := pm.CalculateCost(0, 0)

	if cost.TotalCost != 0 {
		t.Errorf("expected total cost $0 for zero tokens, got $%f", cost.TotalCost)
	}
}

func TestPricingRegistry_LoadDefaults(t *testing.T) {
	pr := NewPricingRegistry()
	pr.LoadDefaults()

	// Check OpenAI models
	_, found := pr.Get("openai", "gpt-4")
	if !found {
		t.Error("expected to find openai/gpt-4 in defaults")
	}

	_, found = pr.Get("openai", "gpt-4-turbo")
	if !found {
		t.Error("expected to find openai/gpt-4-turbo in defaults")
	}

	_, found = pr.Get("openai", "gpt-3.5-turbo")
	if !found {
		t.Error("expected to find openai/gpt-3.5-turbo in defaults")
	}

	// Check Anthropic models
	_, found = pr.Get("anthropic", "claude-3-opus")
	if !found {
		t.Error("expected to find anthropic/claude-3-opus in defaults")
	}

	_, found = pr.Get("anthropic", "claude-3-sonnet")
	if !found {
		t.Error("expected to find anthropic/claude-3-sonnet in defaults")
	}

	_, found = pr.Get("anthropic", "claude-3-haiku")
	if !found {
		t.Error("expected to find anthropic/claude-3-haiku in defaults")
	}
}

func TestPricingRegistry_Update(t *testing.T) {
	pr := NewPricingRegistry()

	// Register initial
	pr.Register(PricingModel{
		Provider:              "test",
		Model:                 "model",
		InputPricePerMillion:  10.0,
		OutputPricePerMillion: 20.0,
	})

	// Update with new price
	pr.Register(PricingModel{
		Provider:              "test",
		Model:                 "model",
		InputPricePerMillion:  15.0,
		OutputPricePerMillion: 25.0,
	})

	retrieved, _ := pr.Get("test", "model")
	if retrieved.InputPricePerMillion != 15.0 {
		t.Errorf("expected updated input price $15.0, got $%f", retrieved.InputPricePerMillion)
	}
	if retrieved.OutputPricePerMillion != 25.0 {
		t.Errorf("expected updated output price $25.0, got $%f", retrieved.OutputPricePerMillion)
	}
}

func TestPricingRegistry_GetAllByProvider(t *testing.T) {
	pr := NewPricingRegistry()

	// Register multiple models for same provider
	pr.Register(PricingModel{Provider: "openai", Model: "gpt-4", InputPricePerMillion: 30.0, OutputPricePerMillion: 60.0})
	pr.Register(PricingModel{Provider: "openai", Model: "gpt-3.5", InputPricePerMillion: 0.5, OutputPricePerMillion: 1.5})
	pr.Register(PricingModel{Provider: "anthropic", Model: "claude-3", InputPricePerMillion: 3.0, OutputPricePerMillion: 15.0})

	// Get all for openai
	openaiModels := pr.GetAllByProvider("openai")
	if len(openaiModels) != 2 {
		t.Errorf("expected 2 openai models, got %d", len(openaiModels))
	}

	// Get all for anthropic
	anthropicModels := pr.GetAllByProvider("anthropic")
	if len(anthropicModels) != 1 {
		t.Errorf("expected 1 anthropic model, got %d", len(anthropicModels))
	}

	// Get all for nonexistent
	nonexistent := pr.GetAllByProvider("nonexistent")
	if len(nonexistent) != 0 {
		t.Errorf("expected 0 models for nonexistent provider, got %d", len(nonexistent))
	}
}
