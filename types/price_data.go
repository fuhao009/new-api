package types

import (
	"fmt"
	"math"

	"github.com/shopspring/decimal"
)

type GroupRatioInfo struct {
	GroupRatio        float64
	GroupSpecialRatio float64
	HasSpecialRatio   bool
}

type PriceData struct {
	FreeModel            bool
	ModelPrice           float64
	ModelRatio           float64
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	CacheCreation5mRatio float64
	CacheCreation1hRatio float64
	ImageRatio           float64
	AudioRatio           float64
	AudioCompletionRatio float64
	otherRatios          map[string]float64
	// ChannelRatio is the optional per-channel promotion multiplier. Nil means
	// absent (1); an explicit 0 makes the channel free. It is applied exactly
	// once after model and group pricing, so it is never part of otherRatios.
	ChannelRatio      *float64
	UsePrice          bool
	Quota             int // 按次计费的最终额度（MJ / Task）
	QuotaToPreConsume int // 按量计费的预消耗额度
	// PreConsumeBeforeRouting retains the unrounded estimate before group and
	// channel multipliers, including when the first selected channel is free.
	PreConsumeBeforeRouting float64
	GroupRatioInfo          GroupRatioInfo
}

func (p *PriceData) AddOtherRatio(key string, ratio float64) {
	if !isValidOtherRatio(ratio) {
		return
	}
	if p.otherRatios == nil {
		p.otherRatios = make(map[string]float64)
	}
	p.otherRatios[key] = ratio
}

func (p *PriceData) ReplaceOtherRatios(ratios map[string]float64) bool {
	p.otherRatios = nil
	for key, ratio := range ratios {
		p.AddOtherRatio(key, ratio)
	}
	return len(p.otherRatios) > 0
}

func (p *PriceData) HasOtherRatio(key string) bool {
	ratio, ok := p.otherRatios[key]
	return ok && isValidOtherRatio(ratio)
}

func (p *PriceData) OtherRatios() map[string]float64 {
	if len(p.otherRatios) == 0 {
		return nil
	}
	ratios := make(map[string]float64, len(p.otherRatios))
	for key, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) {
			ratios[key] = ratio
		}
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

func (p *PriceData) OtherRatioMultiplier() float64 {
	multiplier := 1.0
	for _, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) && ratio != 1.0 {
			multiplier *= ratio
		}
	}
	return multiplier
}

func (p *PriceData) ApplyOtherRatiosToFloat(value float64) float64 {
	return value * p.OtherRatioMultiplier()
}

// OptionalChannelRatio returns the stored promotion ratio, or nil when absent
// or not a finite non-negative number.
func (p *PriceData) OptionalChannelRatio() *float64 {
	if p == nil || p.ChannelRatio == nil {
		return nil
	}
	ratio := *p.ChannelRatio
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
		return nil
	}
	return p.ChannelRatio
}

// EffectiveChannelRatio returns the stored promotion ratio, or 1 when absent.
// Negative and non-finite stored values degrade to 1 so a corrupted setting
// cannot become a credit. Quota conversion separately guards overflow.
func (p *PriceData) EffectiveChannelRatio() float64 {
	ratio := p.OptionalChannelRatio()
	if ratio == nil {
		return 1
	}
	return *ratio
}

// ApplyChannelRatioToFloat applies the channel promotion multiplier once to an
// already group-priced value. A zero ratio intentionally yields a zero charge.
func (p *PriceData) ApplyChannelRatioToFloat(value float64) float64 {
	return value * p.EffectiveChannelRatio()
}

func (p *PriceData) ApplyOtherRatiosToDecimal(value decimal.Decimal) decimal.Decimal {
	for _, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) && ratio != 1.0 {
			value = value.Mul(decimal.NewFromFloat(ratio))
		}
	}
	return value
}

func (p *PriceData) RemoveOtherRatiosFromFloat(value float64) float64 {
	for _, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) && ratio != 1.0 {
			value /= ratio
		}
	}
	return value
}

func isValidOtherRatio(ratio float64) bool {
	return ratio > 0 && !math.IsInf(ratio, 1)
}

func (p *PriceData) ToSetting() string {
	return fmt.Sprintf("ModelPrice: %f, ModelRatio: %f, CompletionRatio: %f, CacheRatio: %f, GroupRatio: %f, UsePrice: %t, CacheCreationRatio: %f, CacheCreation5mRatio: %f, CacheCreation1hRatio: %f, QuotaToPreConsume: %d, ImageRatio: %f, AudioRatio: %f, AudioCompletionRatio: %f", p.ModelPrice, p.ModelRatio, p.CompletionRatio, p.CacheRatio, p.GroupRatioInfo.GroupRatio, p.UsePrice, p.CacheCreationRatio, p.CacheCreation5mRatio, p.CacheCreation1hRatio, p.QuotaToPreConsume, p.ImageRatio, p.AudioRatio, p.AudioCompletionRatio)
}
