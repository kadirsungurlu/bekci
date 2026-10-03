---
id: sign-in-sso
title: Sign-in, password reset and SSO (OpenID Connect)
nav: Sign-in & SSO
description: User email and the "forgot password" flow with system email, single sign-on (OIDC) with Google / Microsoft / Keycloak / Authentik, role mapping and how it relates to two-factor authentication.
section: Usage
order: 14
slug: sign-in-and-sso
---

There are three ways into Bekci: **username + password** (optionally with two-factor authentication), a **password reset link** (by email) and **single sign-on** (a company account via OpenID Connect). All of them are managed under **Settings → Sign-in & SSO** and written to the audit log.

## User email {#email}

Every account may have an optional email: the admin enters it in the user form, the user changes it under **Settings → My account → Email** (the current password is required; the email is the key to password resets). The same email cannot belong to two accounts. Emails are stored lowercase; on the sign-in screen an email is accepted instead of the username (only for "forgot password").

## Forgot password {#forgot-password}

1. First create an **email (SMTP) notification channel** (Notifications → New channel → Email) and confirm it works with **Send test notification**.
2. Select that channel under **Settings → Sign-in & SSO → System email**. Bekci sends reset emails with this channel's SMTP settings; the recipient is the user's own address (the channel's "recipients" are not used).
3. The sign-in screen now shows a **Forgot password** link. A user who enters a username or email receives a link if the account has an email on file.

Security rules:

- The response is always the same ("if the account is linked to an email on file, a link has been sent"); whether the account exists is never leaked.
- 10 requests per IP and 3 per account every 15 minutes; reset attempts are rate-limited per IP too.
- The link is valid for **30 minutes** and **single-use**; only the SHA-256 hash of the token is stored. A new request invalidates the previous link.
- Changing the password **signs the user out everywhere**; two-factor authentication stays on if it was enabled (if the phone is gone as well, an admin uses **Reset 2FA** or the [command line](/en/docs/troubleshooting/)).
- The link's address comes from the `BASE_URL` environment variable; without it the request's host name is used. Set `BASE_URL` behind a reverse proxy.
- Disabled accounts and password-less accounts created through SSO never receive a link.

## Single sign-on (OpenID Connect) {#sso}

Bekci works with any **OpenID Connect** provider: Google Workspace, Microsoft Entra ID (Azure AD), Keycloak, Authentik, Authelia, Okta, Zitadel… The flow is *authorization code* + **PKCE (S256)**, **state** and **nonce** checks, and `id_token` signature verification against the provider's JWKS keys. The client secret is stored in the database and masked in API responses and backups.

### Setup {#sso-setup}

1. Copy the **redirect URI** shown under **Settings → Sign-in & SSO → Single sign-on**: `https://⟦bekci.example.com⟧/api/auth/oidc/callback`. For it to be correct the `BASE_URL` environment variable must be the panel's public address and the reverse proxy must send `X-Forwarded-Proto: https`.
2. Create a **web application** client at your provider, register the redirect URI and obtain the **client ID** and **client secret**.
3. In Bekci enter the **Issuer URL**, client ID and secret; **Test discovery** confirms that `/.well-known/openid-configuration` can be read. Scopes default to `openid profile email`.
4. Choose the account mapping and tick **SSO sign-in enabled**. The sign-in screen now has a **"Sign in with …"** button. Optionally hide the password form (it stays reachable through a link, so admins are never locked out).

### Account mapping and roles {#sso-accounts}

On sign-in the account is resolved in this order:

1. **Previously linked identity** (issuer + subject) → that account.
2. **Local account with a matching email** (if *Link to the local account with a matching email* is on and the provider returns the email as verified) → the account is linked and recognised by subject from then on.
3. **Create account** (if *Create accounts for unknown users* is on): the username is derived from `preferred_username` (or the claim you choose, otherwise the part of the email before @), sanitised, with `-2`, `-3` appended on conflicts. The account has no usable password (random, unusable hash); the user signs in through SSO only. When off, unknown users see "no account is linked to this identity".

**Role mapping** (optional): enter a claim name (e.g. `groups`, `roles`) and the values that grant admin / editor / viewer. Values are comma-separated and case-insensitive; the highest matching role wins. The role is recalculated **on every sign-in** — a user removed from the group is downgraded at the next sign-in; the last active admin is never downgraded by a mapping. Without a match an existing account keeps its role and a new account gets the **default role**. With an empty role claim, roles are managed only in the panel.

Restricted (customer) visibility is not changed by SSO: an account that was restricted stays restricted; newly created accounts see all monitors (as their role allows).

### Two-factor authentication and SSO {#sso-2fa}

Users signing in through SSO are **not asked for Bekci's local TOTP code**; the second factor (phone prompt, security key, conditional access) is the provider's responsibility and should be enforced there. If the same account signs in **with a password**, TOTP is still required. A user who first signs in via SSO with an admin-issued temporary password is not shown the "change password" screen; the temporary password becomes meaningless. Account security endpoints such as API keys and password change behave the same for SSO users.

### Provider examples {#sso-examples}

:::tabs key=idp label="Provider"
@tab Google
- Google Cloud Console → **APIs & Services → Credentials → Create credentials → OAuth client ID**, type **Web application**.
- Authorized redirect URI: `https://⟦bekci.example.com⟧/api/auth/oidc/callback`.
- Bekci: Issuer `https://accounts.google.com`, scopes `openid profile email`.
- Google does not return groups; leave role mapping empty and manage roles in the panel, or — to restrict sign-in to your Workspace domain — turn *Create accounts* off and pre-create the accounts (they are linked by email).
@tab Microsoft
- Entra admin center → **App registrations → New registration**, Redirect URI (Web): `https://⟦bekci.example.com⟧/api/auth/oidc/callback`; create a client secret under **Certificates & secrets**.
- Issuer: `https://login.microsoftonline.com/⟦TENANT-ID⟧/v2.0` (single tenant). For the `email` claim use **Token configuration → Add optional claim → ID → email**.
- For roles define **App roles** and use the `roles` claim (role claim: `roles`; values are the app role's *value*). To use groups instead, **Token configuration → Add groups claim** (`groups`, returns group IDs; put the IDs into the value fields).
@tab Keycloak
- Realm → **Clients → Create client**: Client type OpenID Connect, *Client authentication* on, Valid redirect URI `https://⟦bekci.example.com⟧/api/auth/oidc/callback`.
- Issuer: `https://⟦keycloak.example.com⟧/realms/⟦realm⟧`.
- For groups add a **Group Membership** mapper to the client (Token Claim Name `groups`, *Full group path* off) and set role claim `groups` in Bekci with group names as values (e.g. `bekci-admins`). `preferred_username` and `email` arrive with the standard scopes; the user needs *Email verified* on for email linking.
@tab Authentik
- **Applications → Providers → Create → OAuth2/OpenID Provider**: Client type Confidential, Redirect URI `https://⟦bekci.example.com⟧/api/auth/oidc/callback`, a signing key selected; then create an **Application** bound to this provider.
- Issuer: the provider's **OpenID Configuration Issuer** value (`https://⟦auth.example.com⟧/application/o/⟦slug⟧/`); Bekci strips the trailing `/`.
- Groups arrive in the `groups` claim with the `openid profile email` scopes: role claim `groups`, values are Authentik group names.
:::

### Troubleshooting {#sso-troubleshooting}

| Error on the sign-in screen | Cause / fix |
|---|---|
| *The sign-in session could not be verified* | The 10-minute state expired, cookies are blocked or the page was opened from a different host. The redirect URI and the panel address must match (`BASE_URL`). |
| *Could not obtain tokens from the provider* | Wrong client secret or a different redirect URI registered at the provider. If Test discovery works, the problem is the secret/redirect. |
| *The identity token could not be verified* | The issuer URL does not exactly match the token's `iss` (trailing `/`, `v2.0`, realm name). |
| *No account is linked to this identity* | *Create accounts* is off and no email matched (or the email is not verified). Pre-create the account with its email or enable account creation. |
| *No group grants your account a role* | A role claim is configured, the default role is empty and the user matches none of the values. |

The server log (`LOG_LEVEL=info`) records every failed SSO sign-in as `OIDC girişi başarısız` with its reason; the audit log shows successful sign-ins as **Signed in with SSO** and created accounts as **Account created via SSO**.
