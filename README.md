# payment-fawry

[Fawry](https://developer.fawrystaging.com/docs) driver for togo **payment**.

```bash
togo install togo-framework/payment
togo install togo-framework/payment-fawry
```
```env
PAYMENT_DRIVER=fawry
FAWRY_SECURITY_KEY=...
```

Registers on the togo `payment.PaymentProvider` interface and is selected via
`PAYMENT_DRIVER=fawry`. Gateway API calls are scaffolded — see the Fawry docs.

MIT
