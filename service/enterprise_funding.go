package service

import "github.com/QuantumNous/new-api/model"

const BillingSourceEnterprise = "enterprise"

type EnterpriseFunding struct {
	enterpriseId, userId, tokenId int
	requestId                     string
	consumed                      int
}

func (f *EnterpriseFunding) Source() string { return BillingSourceEnterprise }
func (f *EnterpriseFunding) PreConsume(amount int) error {
	if err := model.ReserveEnterpriseQuota(f.enterpriseId, f.userId, f.tokenId, f.requestId, f.consumed+amount); err != nil {
		return err
	}
	f.consumed += amount
	return nil
}
func (f *EnterpriseFunding) Settle(delta int) error {
	return model.SettleEnterpriseQuota(f.requestId, f.consumed+delta)
}
func (f *EnterpriseFunding) Refund() error { return model.RefundEnterpriseQuota(f.requestId) }
