package usage

// PricingModel represents pricing information for a model
type PricingModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`

	// Input pricing (per 1M tokens)
	InputPricePerMillion float64 `json:"input_price_per_million"`

	// Output pricing (per 1M tokens)
	OutputPricePerMillion float64 `json:"output_price_per_million"`

	// Currency code (default USD)
	Currency string `json:"currency"`
}

// Cost represents the cost breakdown for a request
type Cost struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	InputCost    float64 `json:"input_cost"`
	OutputCost   float64 `json:"output_cost"`
	TotalCost    float64 `json:"total_cost"`
	Currency     string  `json:"currency"`
}

// CalculateCost calculates the cost based on token counts and pricing model
func (pm *PricingModel) CalculateCost(inputTokens, outputTokens int) Cost {
	inputCost := (float64(inputTokens) / 1_000_000) * pm.InputPricePerMillion
	outputCost := (float64(outputTokens) / 1_000_000) * pm.OutputPricePerMillion

	return Cost{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		InputCost:    inputCost,
		OutputCost:   outputCost,
		TotalCost:    inputCost + outputCost,
		Currency:     pm.Currency,
	}
}

// PricingRegistry manages pricing models for multiple providers and models
type PricingRegistry struct {
	models map[string]*PricingModel // "provider/model" -> PricingModel
}

// NewPricingRegistry creates a new pricing registry
func NewPricingRegistry() *PricingRegistry {
	return &PricingRegistry{
		models: make(map[string]*PricingModel),
	}
}

// Register adds or updates a pricing model
func (pr *PricingRegistry) Register(pm PricingModel) {
	key := pm.Provider + "/" + pm.Model
	if pm.Currency == "" {
		pm.Currency = "USD"
	}
	pr.models[key] = &pm
}

// Get retrieves a pricing model
func (pr *PricingRegistry) Get(provider, model string) (*PricingModel, bool) {
	key := provider + "/" + model
	pm, ok := pr.models[key]
	return pm, ok
}

// GetAllByProvider retrieves all pricing models for a specific provider
func (pr *PricingRegistry) GetAllByProvider(provider string) []PricingModel {
	var result []PricingModel
	prefix := provider + "/"
	for key, pm := range pr.models {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, *pm)
		}
	}
	return result
}

// AllModels returns all registered pricing models.
func (pr *PricingRegistry) AllModels() []PricingModel {
	var result []PricingModel
	for _, pm := range pr.models {
		result = append(result, *pm)
	}
	return result
}

// LoadDefaults loads default pricing for common models
func (pr *PricingRegistry) LoadDefaults() {
	// OpenAI pricing (as of 2024)
	pr.Register(PricingModel{
		Provider:              "openai",
		Model:                 "gpt-4",
		InputPricePerMillion:  30.0,
		OutputPricePerMillion: 60.0,
		Currency:              "USD",
	})
	pr.Register(PricingModel{
		Provider:              "openai",
		Model:                 "gpt-4-turbo",
		InputPricePerMillion:  10.0,
		OutputPricePerMillion: 30.0,
		Currency:              "USD",
	})
	pr.Register(PricingModel{
		Provider:              "openai",
		Model:                 "gpt-3.5-turbo",
		InputPricePerMillion:  0.5,
		OutputPricePerMillion: 1.5,
		Currency:              "USD",
	})

	// Anthropic pricing (as of 2024)
	pr.Register(PricingModel{
		Provider:              "anthropic",
		Model:                 "claude-3-opus",
		InputPricePerMillion:  15.0,
		OutputPricePerMillion: 75.0,
		Currency:              "USD",
	})
	pr.Register(PricingModel{
		Provider:              "anthropic",
		Model:                 "claude-3-sonnet",
		InputPricePerMillion:  3.0,
		OutputPricePerMillion: 15.0,
		Currency:              "USD",
	})
	pr.Register(PricingModel{
		Provider:              "anthropic",
		Model:                 "claude-3-haiku",
		InputPricePerMillion:  0.25,
		OutputPricePerMillion: 1.25,
		Currency:              "USD",
	})
}
