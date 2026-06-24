// Package fawry is a Fawry driver for togo payment. Blank-import it and set
// PAYMENT_DRIVER=fawry plus FAWRY_SECURITY_KEY. The driver registers and is env-configured; the
// gateway API calls are scaffolded (see Fawry docs: https://developer.fawrystaging.com/docs) — the togo payment
// interface is satisfied. Contributions to flesh out the calls are welcome.
package fawry

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/togo-framework/payment"
	"github.com/togo-framework/togo"
)

func init() {
	payment.RegisterDriver("fawry", func(k *togo.Kernel) (payment.PaymentProvider, error) {
		key := os.Getenv("FAWRY_SECURITY_KEY")
		if key == "" {
			return nil, errors.New("payment-fawry: FAWRY_SECURITY_KEY not set")
		}
		return &provider{key: key, hc: &http.Client{Timeout: 20 * time.Second}}, nil
	})
}

type provider struct {
	key string
	hc  *http.Client
}

var errTODO = errors.New("payment-fawry: this operation is scaffolded — wire the Fawry API (https://developer.fawrystaging.com/docs)")

func (p *provider) CreateCharge(context.Context, payment.ChargeRequest) (*payment.Charge, error) {
	return nil, errTODO
}
func (p *provider) Refund(context.Context, payment.RefundRequest) error { return errTODO }
func (p *provider) CreateCheckoutSession(context.Context, payment.CheckoutRequest) (*payment.CheckoutSession, error) {
	return nil, errTODO
}
func (p *provider) CreateCustomer(context.Context, payment.Customer) (string, error) { return "", errTODO }
func (p *provider) CreateSubscription(context.Context, payment.SubscriptionRequest) (*payment.Subscription, error) {
	return nil, errTODO
}
func (p *provider) HandleWebhook(context.Context, map[string]string, []byte) (*payment.WebhookEvent, error) {
	return nil, errTODO
}
