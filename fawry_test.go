package fawry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/togo-framework/payment"
)

func newTestProvider(h http.HandlerFunc) (*provider, *httptest.Server) {
	srv := httptest.NewServer(h)
	return &provider{code: "MC", key: "SEC", base: srv.URL, hc: srv.Client()}, srv
}

func TestSignatureDeterministic(t *testing.T) {
	p := &provider{code: "MC", key: "SEC"}
	a := p.chargeSignature("ref1", "", "PAYATFAWRY", 1500, "")
	b := p.chargeSignature("ref1", "", "PAYATFAWRY", 1500, "")
	if a == "" || a != b {
		t.Errorf("signature not stable: %q vs %q", a, b)
	}
	if c := p.chargeSignature("ref2", "", "PAYATFAWRY", 1500, ""); c == a {
		t.Error("signature should change with ref")
	}
}

func TestCreateChargeSendsSignature(t *testing.T) {
	var seen map[string]any
	p, srv := newTestProvider(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ECommerceWeb/Fawry/payments/charge" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&seen)
		json.NewEncoder(w).Encode(map[string]any{"statusCode": "200", "referenceNumber": "9988"})
	})
	defer srv.Close()
	ch, err := p.CreateCharge(context.Background(), payment.ChargeRequest{Amount: payment.Money{Amount: 1500, Currency: "EGP"}, Metadata: map[string]string{"merchantRefNum": "ref1"}})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID != "9988" || ch.Status != "pending" {
		t.Errorf("got %+v", ch)
	}
	want := p.chargeSignature("ref1", "", "PAYATFAWRY", 1500, "")
	if seen["signature"] != want {
		t.Errorf("signature: got %v want %v", seen["signature"], want)
	}
	if seen["paymentMethod"] != "PAYATFAWRY" {
		t.Errorf("method = %v", seen["paymentMethod"])
	}
}

func TestChargeRejectsBadStatus(t *testing.T) {
	p, srv := newTestProvider(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"statusCode": "9901", "statusDescription": "invalid"})
	})
	defer srv.Close()
	if _, err := p.CreateCharge(context.Background(), payment.ChargeRequest{Amount: payment.Money{Amount: 100}, Metadata: map[string]string{"merchantRefNum": "r"}}); err == nil {
		t.Error("expected error on non-200 statusCode")
	}
}

func TestWebhookSignature(t *testing.T) {
	p := &provider{code: "MC", key: "SEC"}
	m := map[string]any{
		"fawryRefNumber": "F1", "merchantRefNumber": "M1", "paymentAmount": 15.0, "orderAmount": 15.0,
		"orderStatus": "PAID", "paymentMethod": "CARD", "paymentRefrenceNumber": "P1",
	}
	want := sha256hex("F1" + "M1" + "15.00" + "15.00" + "PAID" + "CARD" + "P1" + "SEC")
	m["messageSignature"] = want
	body, _ := json.Marshal(m)
	ev, err := p.HandleWebhook(context.Background(), nil, body)
	if err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if ev.Type != "order.paid" || ev.ID != "F1" {
		t.Errorf("got %+v", ev)
	}
	m["messageSignature"] = "wrong"
	bad, _ := json.Marshal(m)
	if _, err := p.HandleWebhook(context.Background(), nil, bad); err == nil {
		t.Error("bad signature accepted")
	}
}
