# Afterword web

Cloud dashboard for Afterword. Next.js app-router client that talks to the Go API.

## Surfaces

- OTP sign-in against `/v1/auth/otp/*` and `/v1/auth/refresh`
- Meeting library with cursor pagination against `/v1/meetings`
- Meeting detail with summary, transcript and cited Ask against `/v1/meetings/{id}`
- Public share viewer against `/v1/share/{token}`

## Configuration

Set `NEXT_PUBLIC_API_BASE_URL` to the API origin. See `.env.example`.

## Local development

```
pnpm install
pnpm dev
```

The dev server listens on port 3200.

## Production

```
pnpm build
pnpm start
```
