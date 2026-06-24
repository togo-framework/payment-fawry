// Package fawry is a Fawry (Egypt) driver for togo payment. Set
// PAYMENT_DRIVER=fawry + FAWRY_MERCHANT_CODE + FAWRY_SECURITY_KEY. Optional
// FAWRY_BASE_URL (default: live; use https://atfawry.fawrystaging.com for sandbox).
//
// Requests are signed with SHA-256 over a documented field concatenation +
// the security key; webhook notifications are verified the same way.
package fawry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/togo-framework/payment"
	"github.com/togo-framework/togo"
)

const defaultAPI = "https://www.atfawry.com"

func init() {
	payment.RegisterDriver("fawry", func(k *togo.Kernel) (payment.PaymentProvider, error) {
		code := os.Getenv("FAWRY_MERCHANT_CODE")
		key := os.Getenv("FAWRY_SECURITY_KEY")
		if code == "" || key == "" {
			return nil, errors.New("payment-fawry: FAWRY_MERCHANT_CODE and FAWRY_SECURITY_KEY are required")
		}
		base := os.Getenv("FAWRY_BASE_URL")
		if base == "" {
			base = defaultAPI
		}
		return &provider{code: code, key: key, base: strings.TrimRight(base, "/"), hc: &http.Client{Timeout: 20 * time.Second}}, nil
	})
}

type provider struct {
	code string
	key  string
	base string
	hc   *http.Client
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func amt(minor int64) string { return strconv.FormatFloat(float64(minor)/100, 'f', 2, 64) }

func (p *provider) post(ctx context.Context, path string, payload any) (map[string]any, error) {
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if len(b) > 0 {
		_ = json.Unmarshal(b, &m)
	}
	if resp.StatusCode >= 300 {
		return m, fmt.Errorf("fawry: %s: %s", resp.Status, str(m["statusDescription"]))
	}
	// Fawry returns statusCode 200 on success inside the body.
	if sc := str(m["statusCode"]); sc != "" && sc != "200" {
		return m, fmt.Errorf("fawry: statusCode %s: %s", sc, str(m["statusDescription"]))
	}
	return m, nil
}

func status(statusCode string) string {
	switch statusCode {
	case "200":
		return "pending" // charge accepted; PAID arrives via webhook
	case "":
		return "pending"
	default:
		return "failed"
	}
}

func (p *provider) chargeSignature(ref, profileID, method string, amount int64, token string) string {
	// merchantCode + merchantRefNum + customerProfileId + paymentMethod + amount(2dp) + cardToken + securityKey
	return sha256hex(p.code + ref + profileID + method + amt(amount) + token + p.key)
}

func (p *provider) CreateCharge(ctx context.Context, r payment.ChargeRequest) (*payment.Charge, error) {
	ref := metaOr(r.Metadata, "merchantRefNum", "ref-"+ref8(r))
	method := "CARD"
	if r.Token == "" {
		method = "PAYATFAWRY"
	}
	profileID := r.Customer.ID
	body := map[string]any{
		"merchantCode":      p.code,
		"merchantRefNum":    ref,
		"customerProfileId": profileID,
		"customerName":      r.Customer.Name,
		"customerMobile":    r.Customer.Phone,
		"customerEmail":     r.Customer.Email,
		"paymentMethod":     method,
		"amount":            float64(r.Amount.Amount) / 100,
		"currencyCode":      orDefault(r.Amount.Currency, "EGP"),
		"description":       r.Description,
		"chargeItems":       []map[string]any{{"itemId": ref, "description": r.Description, "price": float64(r.Amount.Amount) / 100, "quantity": 1}},
		"signature":         p.chargeSignature(ref, profileID, method, r.Amount.Amount, r.Token),
	}
	if r.Token != "" {
		body["cardToken"] = r.Token
	}
	m, err := p.post(ctx, "/ECommerceWeb/Fawry/payments/charge", body)
	if err != nil {
		return nil, err
	}
	id := str(m["referenceNumber"])
	if id == "" {
		id = ref
	}
	return &payment.Charge{ID: id, Status: status(str(m["statusCode"])), Amount: r.Amount, Provider: "fawry", Raw: m}, nil
}

func (p *provider) Refund(ctx context.Context, r payment.RefundRequest) error {
	if r.ChargeID == "" {
		return errors.New("fawry: RefundRequest.ChargeID (the Fawry reference number) is required")
	}
	var refund string
	if r.Amount != nil {
		refund = amt(r.Amount.Amount)
	}
	// signature = merchantCode + referenceNumber + refundAmount + reason + securityKey
	sig := sha256hex(p.code + r.ChargeID + refund + "" + p.key)
	body := map[string]any{"merchantCode": p.code, "referenceNumber": r.ChargeID, "signature": sig}
	if refund != "" {
		body["refundAmount"] = float64(r.Amount.Amount) / 100
	}
	_, err := p.post(ctx, "/ECommerceWeb/Fawry/payments/refund", body)
	return err
}

func (p *provider) CreateCheckoutSession(ctx context.Context, r payment.CheckoutRequest) (*payment.CheckoutSession, error) {
	// Hosted checkout = a PAYATFAWRY charge; the customer pays at a Fawry outlet or
	// the returned URL. We create the charge and surface the reference + checkout URL.
	ref := metaOr(r.Metadata, "merchantRefNum", "co-"+coRef(r))
	body := map[string]any{
		"merchantCode":   p.code,
		"merchantRefNum": ref,
		"customerName":   r.Customer.Name,
		"customerMobile": r.Customer.Phone,
		"customerEmail":  r.Customer.Email,
		"paymentMethod":  "PAYATFAWRY",
		"amount":         float64(total(r)) / 100,
		"currencyCode":   orDefault(r.Amount.Currency, "EGP"),
		"chargeItems":    items(r),
		"returnUrl":      r.SuccessURL,
		"signature":      p.chargeSignature(ref, r.Customer.ID, "PAYATFAWRY", total(r), ""),
	}
	m, err := p.post(ctx, "/ECommerceWeb/Fawry/payments/charge", body)
	if err != nil {
		return nil, err
	}
	url := str(m["paymentURL"])
	if url == "" {
		url = p.base + "/atfawry/plugin/transaction/refNo/" + str(m["referenceNumber"])
	}
	return &payment.CheckoutSession{ID: str(m["referenceNumber"]), URL: url}, nil
}

func (p *provider) CreateCustomer(context.Context, payment.Customer) (string, error) {
	return "", errors.New("fawry: customers are passed inline per charge — no standalone customer API")
}

func (p *provider) CreateSubscription(context.Context, payment.SubscriptionRequest) (*payment.Subscription, error) {
	return nil, errors.New("fawry: native subscriptions are not wired — use the togo subscriptions plugin")
}

// HandleWebhook verifies the Fawry notification signature and maps the order status.
func (p *provider) HandleWebhook(_ context.Context, _ map[string]string, body []byte) (*payment.WebhookEvent, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("fawry: bad webhook body: %w", err)
	}
	// signature = fawryRefNumber + merchantRefNum + paymentAmount(2dp) + orderAmount(2dp)
	//           + orderStatus + paymentMethod + paymentRefrenceNumber + securityKey
	want := sha256hex(
		str(m["fawryRefNumber"]) + str(m["merchantRefNumber"]) +
			dec2(m["paymentAmount"]) + dec2(m["orderAmount"]) +
			str(m["orderStatus"]) + str(m["paymentMethod"]) + str(m["paymentRefrenceNumber"]) + p.key,
	)
	if got := str(m["messageSignature"]); got != "" && !strings.EqualFold(got, want) {
		return nil, errors.New("fawry: webhook signature mismatch")
	}
	return &payment.WebhookEvent{
		Type:     "order." + strings.ToLower(str(m["orderStatus"])),
		ID:       str(m["fawryRefNumber"]),
		Provider: "fawry",
		Raw:      m,
	}, nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func dec2(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', 2, 64)
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return strconv.FormatFloat(f, 'f', 2, 64)
		}
		return n
	default:
		return ""
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func metaOr(m map[string]string, k, d string) string {
	if v, ok := m[k]; ok && v != "" {
		return v
	}
	return d
}

func total(r payment.CheckoutRequest) int64 {
	if len(r.Items) == 0 {
		return r.Amount.Amount
	}
	var t int64
	for _, it := range r.Items {
		q := it.Quantity
		if q == 0 {
			q = 1
		}
		t += it.Amount.Amount * q
	}
	return t
}

func items(r payment.CheckoutRequest) []map[string]any {
	if len(r.Items) == 0 {
		return []map[string]any{{"itemId": "item", "price": float64(r.Amount.Amount) / 100, "quantity": 1}}
	}
	out := make([]map[string]any, 0, len(r.Items))
	for i, it := range r.Items {
		q := it.Quantity
		if q == 0 {
			q = 1
		}
		out = append(out, map[string]any{"itemId": fmt.Sprintf("item-%d", i), "description": it.Name, "price": float64(it.Amount.Amount) / 100, "quantity": q})
	}
	return out
}

func ref8(r payment.ChargeRequest) string {
	if r.Customer.Email != "" {
		return r.Customer.Email
	}
	return fmt.Sprintf("%d", r.Amount.Amount)
}

func coRef(r payment.CheckoutRequest) string {
	if r.Customer.Email != "" {
		return r.Customer.Email
	}
	return fmt.Sprintf("%d", total(r))
}
