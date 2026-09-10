# Accounts (users)

Goal: register, sign in/out, and reset your password.

## Registration

If the admin allows sign-ups (**Hub → Site settings → General → Sign-up**),
new accounts are created on:

![The registration page](../assets/users/10-accounts-register.png)

1. Fill in **first name**, **last name**, **e-mail**, and a password.
2. Accept the terms and choose **Create account**.
3. Depending on the admin's setup you either sign in directly or receive an
   activation e-mail.

## Signing in

![The login page](../assets/users/10-accounts-login.png)

- Enter e-mail + password → **Sign in**.
- Failed attempts are rate-limited; repeated failures lock the account
  temporarily (admin can see it under
  [Suspended users](../admins/02-users.md)).

## Password

- **Change password**: My settings → Change password
  (`/hub#/mysettings.password`).
- **Forgot password**: use the reset link on the login page — the reset
  e-mail requires the instance's [SMTP configuration](../admins/04-site-settings.md#email--smtp)
  to be set by the admin.

## Security tips

- Use a password manager for the instance credential.
- Review **Sessions** (`/hub#/mysettings.sessions`) and sign out devices
  you no longer use.
- Report lost access to your institution admin — they can
  [reset/restore accounts](../admins/02-users.md).

***

Verified against: OlliTeX @ `f827b5ceb9` (2026-09-16)
