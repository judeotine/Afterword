# Afterword Cloud billing

Credits are minutes. A workspace holds one pooled balance, shared by every
member, computed as `SUM(credit_ledger.delta_minutes)` for the workspace. There
are no seats and no subscriptions: a workspace receives a free monthly grant and
tops up with one-off mobile-money or card payments.

Every write to `credit_ledger` goes through `internal/credits.Ledger`. The
ledger takes a per-workspace row lock (`credit_locks`) before it inserts, and a
debit that would take the balance below zero is refused inside the same
transaction, so concurrent debits can never overdraw.

## Metering

| Activity | Cost |
|---|---|
| Meeting-bot minute | 1 credit |
| Cloud transcription minute of uploaded audio | 1 credit |
| Desktop on-device transcription | 0 |
| Hosted summary | 0.1 credit per transcript minute, rounded up per meeting |
| Bring-your-own-key summary | 0 |
| Ask query | 1 credit |

Ledger reasons: `grant`, `purchase`, `bot_usage`, `transcribe_usage`,
`summary_usage`, `ask_usage`, `refund`, `adjust`. The four `*_usage` reasons are
the ones the grant-expiry calculation treats as consumption.

## Packs

Seeded by migration `0013_seed_credit_packs` into `credit_packs`, so prices can
be changed in the database without a deploy. Only rows with `active = true` are
listed or accepted at checkout.

| Pack | Minutes | Price | Currency |
|---|---|---|---|
| Starter 100 | 100 | 15,000 | UGX |
| Team 500 | 500 | 60,000 | UGX |
| Business 2000 | 2,000 | 200,000 | UGX |

Prices are stored in the minor unit of the currency. UGX has no minor unit, so
`price_minor` is the shilling amount.

## Free monthly grant

`FREE_GRANT_MINUTES` (default 300) is granted to every workspace once per
calendar month, in UTC. The grant is recorded in `credit_grants`, one row per
`(workspace_id, period)` where `period` is the first day of the UTC month, with
a unique constraint that makes a second grant for the same month impossible.
The grant row and its `grant` ledger entry are written in one transaction.

Purchased credits never expire. The grant does: when a grant reaches its
`expires_at` (the first instant of the following month), the scheduler takes
back the part of it the workspace did not use.

The amount taken back is

```
unused  = grant_minutes - already_expired_minutes - usage_since_the_grant
expired = min(current_balance, max(unused, 0))
```

where `usage_since_the_grant` is the sum of the `*_usage` debits recorded since
the grant row was created. Clamping to the current balance is what keeps
purchased credits safe: a workspace that spent its grant and then bought a pack
loses nothing, and the balance can never be pushed negative. The claw-back is a
single `adjust` ledger entry with a negative delta, referenced as
`grant_expiry:<grant_id>`, written in the same transaction that stamps
`expired_at` and `expired_minutes` on the grant row.

`usage_since_the_grant` counts every `*_usage` debit recorded after the grant
row was written, whichever balance actually paid for it. A workspace that had
purchased credits when the grant landed therefore has some of that spend
attributed to the grant, which shrinks the claw-back in the customer's favour.
That is deliberate: the alternative, tracking which entry drew on which bucket,
means a lot-based ledger, and this rule can never take more than the customer
has or more than the grant was worth.

Skipped months are safe. If the API is down for two months, the next run expires
every due grant it finds, oldest first, and then grants the current month.

## Scheduling

`internal/jobs.Scheduler` runs the grant task from the API process itself. Each
tick takes `pg_try_advisory_lock` on the task's lock key, so with several API
containers pointed at one database exactly one of them does the work and the
others log and move on. The lock is released as soon as the tick finishes.
`GRANT_INTERVAL` (default 1h) sets how often the tick fires; the task is
idempotent, so running it more often changes nothing.

The share-link request sweep from `internal/meetings` is registered on the same
scheduler, hourly, under its own lock key.

## Entitlements

`billing.Entitlements.RequireCredits(ctx, workspace, estimateMinutes)` is the one
gate. It reads the balance, and when it falls short it returns an error carrying
the shortfall and a top-up link, which the API renders as

```
402 {"error": {"code": "insufficient_credits",
               "message": "...",
               "top_up_url": "{APP_BASE_URL}/billing/top-up"}}
```

Two paths use it today. `POST /v1/bot-jobs` requires the job's estimate
(`estimated_minutes`, default 60) before it writes the row: the estimate is
checked, never spent, because the bot bills the minutes it actually used when it
leaves. Scheduling a bot spends the workspace's credits, so `POST /v1/bot-jobs`
needs the `admin` or `owner` role; `GET /v1/bot-jobs` is open to every member. `POST /v1/meetings/{id}/finalize` prices the work it is about to queue
and requires the total before it touches the meeting, so a refused finalize
leaves the meeting's status and generation exactly where they were and queues
nothing.

Finalize charges only work it actually queued. It asks the job queue which kinds
already exist at this generation, prices only the rest, and debits **before** it
enqueues, so a job can never exist without its charge; if the enqueue then
fails, the exact debit is refunded as a ledger `adjust` referencing the job's
idempotency key, which is also the debit's `ref_id`. A re-finalize that creates
nothing charges nothing.

### What finalize is allowed to price

`meetings.duration_s` is whatever the client said at `POST /v1/meetings`. It is
display metadata and **nothing is ever priced from it**: a client that declares
zero would otherwise transcribe three hours for free, and one that declares a
huge number would overflow the arithmetic into a negative, also free. The
column is clamped to 24 hours at the API boundary and every price is computed in
`int64` with a ceiling helper that cannot overflow.

Billable minutes are derived server-side from the audio object's size, as
reported by the storage `HEAD` that finalize already performs:

```
bytesPerMinute      = 192000 * 60          = 11,520,000
transcribeMinutes   = max(ceil(sizeBytes / bytesPerMinute), 1)   for any audio object
summaryMinutes      = max(ceil(transcribeMinutes / 10), 1)       for any queued summary
```

192,000 bytes per second is 48 kHz, 16-bit, stereo PCM: the densest audio the
upload path accepts. Dividing by the densest encoding gives the **smallest**
duration those bytes could possibly represent, so the figure is a floor that can
never overbill — a three-hour Opus recording at 24 kbps prices as three minutes,
not a hundred and eighty. Both results are clamped to 24 hours' worth of
minutes.

This is deliberately a floor charge, not the price. It exists so that queueing
cloud work is never free and never unbounded; the transcription worker knows the
real duration and reconciles against the ledger in Phase C, where the entry
written here — keyed by the job's idempotency key — is the row to adjust. Any
paid transcribe costs at least one credit, and so does any hosted summary,
including a transcript-only meeting where there is no audio to measure.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/v1/billing/packs` | bearer | Active packs, cheapest first |
| GET | `/v1/billing/balance` | bearer + workspace | `{minutes, grant_expires_at, free_grant_minutes}` |
| POST | `/v1/billing/checkout` | bearer + workspace | `{pack_id, phone?}` → `{payment_id, redirect_url?, push_sent}` |
| GET | `/v1/billing/payments` | bearer + workspace | Payment history, cursor paged |
| POST | `/v1/billing/webhooks/{provider}` | provider signature | Settles a payment |
| POST | `/v1/admin/workspaces/{id}/credits/adjust` | `X-Admin-Token` | `{delta_minutes, ref_id?}` → the new balance |
| POST | `/v1/bot-jobs` | bearer + workspace + admin | `{meeting_url, platform?, scheduled_at?, estimated_minutes?, bot_name?}` → the scheduled row |
| GET | `/v1/bot-jobs` | bearer + workspace | Bot jobs, cursor paged |
| GET | `/v1/bot-jobs/{id}` | bearer + workspace | One bot job |

Spending money is an admin act: `POST /v1/billing/checkout` and
`GET /v1/billing/payments` require the `admin` or `owner` role, while
`GET /v1/billing/balance` and `GET /v1/billing/packs` are open to every member,
so a member can see what the workspace has without being able to spend or to
read its payment history.

The admin route is not part of the member-facing API. It is guarded by the
`ADMIN_TOKEN` environment variable, compared in constant time over SHA-256
digests so neither the token nor its length leaks through timing. When
`ADMIN_TOKEN` is unset the route answers 404 rather than advertising itself.
Every adjustment writes an `audit_log` row under `billing.credits.adjust`, in
the same transaction as the ledger entry, naming the actor as `admin-token`
along with the delta, the ledger entry id and the caller's reference: the
adjustment and its audit trail commit together or not at all.

### Checkout

`POST /v1/billing/checkout` reads the pack, inserts a `pending` `payments` row
whose `provider_ref` is initially the payment's own id, and then asks the
provider to start the payment. If the provider returns a different reference,
the row is updated to carry it. If the provider call fails, the row is marked
`failed` and the caller gets the provider's error. A phone number turns the
checkout into a mobile-money push; without one the provider returns a redirect
URL for a card page.

### Webhooks

The webhook handler verifies the provider's signature over the raw request body
before anything else, then opens one transaction that:

1. locks the `payments` row by `(provider, provider_ref)` with `FOR UPDATE`;
2. returns 200 with `duplicate: true` if the row is no longer `pending`;
3. writes the `purchase` ledger entry through `credits.Ledger` and stamps
   `status`, `paid_at`, and the raw provider payload on the row.

Because the row lock is taken first, duplicate and concurrent deliveries of the
same event credit the workspace exactly once. Duplicates answer 200 so the
provider stops retrying. A body that does not match a known payment answers 404;
a bad signature answers 401.

Only a `paid` event on an already `paid` row is a duplicate. Three other
outcomes move the row to `needs_review` instead, each logged at error level and
recorded in `audit_log` under `billing.payment.needs_review`, and each still
answering 200 so the provider stops retrying:

- a `paid` event for a row that is `failed` or `refunded` — the money may be
  real and the row says otherwise, most often because the reaper failed a
  payment the provider confirmed late;
- a `paid` event whose amount or currency does not match the stored payment,
  which includes an event that reports no amount at all: silence is not a match;
- anything else that cannot be settled safely.

`needs_review` is a terminal state for the automatic paths: the reaper ignores
it, the settlement query refuses it, and nothing credits it. It is a queue for a
human, who reconciles against the provider and adjusts the ledger through the
admin endpoint.

`SettlePayment` only ever moves a row from `pending` to `paid` or `failed`. A
refund is not a settlement and does not travel this path.

A checkout that cannot reach the provider — a timeout, a connection failure, a
5xx — leaves the payment `pending`, because the provider may still have created
it, and the reaper will close it out if no webhook arrives. Only an explicit
refusal (a 4xx from the provider, or a request we would not send) marks the row
`failed` immediately.

Starting a checkout is rate limited per workspace: `CHECKOUT_RATE_LIMIT`
(default 10) in `CHECKOUT_RATE_WINDOW` (default 1h), counted and inserted under
one advisory lock so the count cannot be raced, answering 429 beyond that. A
mobile-money push costs the recipient's attention, and an unauthenticated
attacker who has stolen an admin session should not be able to spray them.

Phone numbers reaching checkout go through `auth.NormalizePhone`, the same
normaliser the OTP path uses, so the provider always sees one canonical form.

### Stale pending payments

A checkout writes a `pending` row before it calls the provider. If the provider
never confirms — the push was ignored, the card page was abandoned, the webhook
was lost — that row would sit `pending` forever. An hourly leader-locked task
marks every `pending` payment older than `PAYMENT_PENDING_TTL` (default 24h) as
`failed`, recording the reason in the row's `raw` payload and logging each one.
It never touches the ledger: a payment that was never credited has nothing to
reverse, and a payment that was credited is no longer `pending`. If a provider
confirms after the reaper has been through, the webhook finds a non-pending row
and is handled by the settlement rules above rather than crediting twice.

## Providers

`internal/payments.PaymentProvider` is the seam:

```go
type PaymentProvider interface {
    StartPayment(ctx context.Context, req StartPaymentRequest) (StartPaymentResponse, error)
    VerifyWebhook(headers http.Header, body []byte) (WebhookEvent, error)
    Refund(ctx context.Context, providerRef string, amount Money) error
}
```

Providers are held in a registry keyed by name; `PAYMENT_PROVIDER` names the one
checkout uses, and the webhook route dispatches by the `{provider}` path
segment, so a provider being swapped does not strand webhooks already in flight
for the old one.

### fake

For development and tests only, and it will not load by accident. The provider
registers only when `ALLOW_FAKE_PAYMENTS=true` is set explicitly alongside
`FAKE_PAYMENT_SECRET`, and `PAYMENT_PROVIDER=fake` is rejected at startup
without that flag. `PAYMENT_PROVIDER` itself defaults to `none`, so a
deployment that says nothing about payments serves packs, balances and history
and answers 503 `payments_unavailable` at checkout rather than quietly handing
out free credits through a test provider. `StartPayment` echoes the reference back as
the provider reference and reports a push when a phone number is present, a
redirect URL otherwise. `VerifyWebhook` requires the shared secret
(`FAKE_PAYMENT_SECRET`) in the `X-Fake-Signature` header, compared in constant
time, and reads `{"payment_id": "...", "status": "paid"}` from the body;
`status` defaults to `paid` when it is absent. Marking a payment paid in
development is therefore:

```
curl -X POST http://localhost:8080/v1/billing/webhooks/fake \
  -H 'X-Fake-Signature: <FAKE_PAYMENT_SECRET>' \
  -d '{"payment_id":"<payment id from checkout>","amount_minor":15000,"currency":"UGX"}'
```

`ALLOW_FAKE_PAYMENTS` registers the provider, and registering it keeps
`POST /v1/billing/webhooks/fake` live whatever `PAYMENT_PROVIDER` says: the
webhook route dispatches on the provider named in the path, not on the
configured default, so anyone holding `FAKE_PAYMENT_SECRET` can mark a payment
paid even on a deployment whose checkout runs through Nylon Pay. **Never set
`ALLOW_FAKE_PAYMENTS` in production**, and treat `FAKE_PAYMENT_SECRET` as a
credential that mints credits. When the fake provider
registers, the API logs a warning at startup. `deploy/docker-compose.dev.yml`
and CI set it; `deploy/api.env.example` sets it to false.

### nylonpay

A skeleton. It compiles, it is tested against fixtures, and it is wired to the
registry, but every detail of the wire protocol below is a **placeholder that
must be confirmed against the Nylon Pay documentation** before the provider is
switched on. The founder owes us: the docs or repo path, sandbox credentials,
the real webhook signature scheme, the supported currencies, the refund API, and
whether Nylon Pay can also send SMS.

Assumptions to confirm against Nylon Pay docs:

1. **Start endpoint.** `POST {NYLONPAY_BASE_URL}/payments`. Neither the path nor
   the method is confirmed.
2. **Request body.** JSON `{amount, currency, phone?, reference, callback_url}`,
   with `amount` in the minor unit as an integer and `currency` an ISO 4217
   alphabetic code. Whether Nylon Pay wants minor units or a decimal string, and
   whether it names the fields this way, is not confirmed.
3. **Authentication.** `Authorization: Bearer {NYLONPAY_API_KEY}`. It may
   instead be a custom header, basic auth, or a signed request.
4. **Idempotency.** An `Idempotency-Key` header carrying our reference. Whether
   Nylon Pay honours it is not confirmed.
5. **Start response.** JSON `{provider_ref, redirect_url?, push_sent?}`, with a
   2xx status meaning accepted. A missing `provider_ref` is treated as a
   provider failure. The real field names are not confirmed.
6. **Webhook signature.** Header `X-Nylon-Signature` carrying the lowercase hex
   HMAC-SHA256 of the exact raw request body under `NYLONPAY_WEBHOOK_SECRET`. A
   `sha256=` prefix is tolerated. Neither the header name, the encoding (hex vs
   base64), the signed payload (raw body vs a canonical string vs a
   timestamp-prefixed body), nor the presence of a replay-protection timestamp
   is confirmed.
7. **Webhook body.** JSON `{provider_ref, reference, status, amount, currency}`
   with `status` one of `pending`, `paid`, `failed`, `refunded`. The real status
   vocabulary is almost certainly different and will need mapping.
8. **Refunds.** Not implemented. `Refund` returns
   `payments.ErrRefundUnsupported` until the refund API is documented.
9. **Currencies.** Only UGX is exercised. Whether Nylon Pay settles other
   currencies, and how it reports them, is not confirmed.

When the real documentation arrives, the changes are confined to
`internal/payments/nylonpay.go` and this section; nothing above the provider
interface should need to move.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `PAYMENT_PROVIDER` | `none` | `none`, `fake` or `nylonpay`; the provider checkout uses |
| `ALLOW_FAKE_PAYMENTS` | `false` | Must be true before the fake provider will register |
| `CHECKOUT_RATE_LIMIT` | `10` | Checkouts a workspace may start per window |
| `CHECKOUT_RATE_WINDOW` | `1h` | The window that limit is counted over |
| `FREE_GRANT_MINUTES` | `300` | Monthly grant per workspace; `0` disables grants |
| `GRANT_INTERVAL` | `1h` | How often the leader-locked grant task ticks |
| `ADMIN_TOKEN` | unset | Enables the admin adjust route; at least 32 characters |
| `PAYMENT_PENDING_TTL` | `24h` | How long a `pending` payment may wait before the reaper fails it |
| `FAKE_PAYMENT_SECRET` | unset | The fake provider's shared secret; at least 16 characters |
| `NYLONPAY_BASE_URL` | unset | Absolute http(s) base URL of the Nylon Pay API |
| `NYLONPAY_API_KEY` | unset | Nylon Pay API key |
| `NYLONPAY_WEBHOOK_SECRET` | unset | HMAC key for webhook verification; at least 16 characters |

`API_BASE_URL` builds the `callback_url` sent to the provider
(`{API_BASE_URL}/v1/billing/webhooks/{provider}`) and `APP_BASE_URL` builds the
customer-facing top-up links.

A provider is only registered when its own credentials are present. If
`PAYMENT_PROVIDER` names a provider with no credentials, the API still serves
packs, balances, and payment history, and checkout answers 503; nothing else is
affected.
