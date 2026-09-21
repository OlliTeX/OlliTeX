# `go/libraries/accesstoken` — access-token encryption (v3)

Go 1:1 port of **`libraries/access-token-encryptor`** (npm
`@overleaf/access-token-encryptor` v3.0.0): symmetric **AES-256-CTR**
encryption of JSON access tokens with **HKDF-SHA512** key derivation from a
per-scheme password. The sync-bridge services (dropbox / github / webdav /
gitbridge) protect their HTTP endpoints with these tokens.

## The wire format (v3 scheme, label-prefixed)
```
<label>:<salt 16 bytes hex>:<ciphertext base64>:<iv 16 bytes hex>
```
`key = HKDF-SHA512(password_utf8, salt, info="", L=32)`; the plaintext is
AES-256-CTR under that key with the embedded IV. Encryption is **randomized**
(a fresh salt+IV per call), so the same plaintext yields a different token on
every call — pinned by the Node suite.

## The API
| Symbol | Purpose |
| --- | --- |
| `type Config struct{ CipherLabel string; CipherPasswords map[string]string; PasswordOrder []string }` | mirrors Node's `{ cipherLabel, cipherPasswords: { <label>: <password> } }`; `PasswordOrder` reproduces Node's insertion-order iteration so multi-scheme validation failures are deterministic/Node-ordered |
| `func New(cfg Config) (*Encryptor, error)` | build the encryptor, running every validation in Node's source order (empty label → colon in label → no `vNN` suffix → missing password → password too short (<16 UTF-16 units) → unknown version → unknown default label) |
| `(*Encryptor).EncryptJson(v any) (string, error)` | marshal with the default scheme, fresh random salt+IV, return `label:saltHex:cipherB64:ivHex` |
| `(*Encryptor).DecryptToJson(encrypted string) (any, error)` | split label, dispatch to the scheme, CTR-decrypt, `JSON.parse` (a bad token → `error decrypting token`) |

## Conventions / gotchas
- **The v3 scheme is the only implementable version** — `New` rejects any label
  whose suffix isn't `-v3` (`unknown version '<v>' for <label>`).
- **Password length is a JS string length** (UTF-16 code units), not bytes —
  `utf16Len` mirrors `password.length`; the threshold is 16.
- **CTR has no auth tag** — a corrupted ciphertext surfaces as a `JSON.parse`
  failure (`error decrypting token`), reproduced by `DecryptToJson`.
- The test oracle used a 38‑`'4'` password (the Node suite's value); a 46‑`'4'`
  test fixture was a red herring (see HANDOFF LIB-08).

## Testing & coverage
`go test ./go/libraries/accesstoken/ -count=1 -cover` — oracle-pinned to the
Node `access-token-encryptor` suite (round-trip, every `New` validation message,
multi-scheme ordering, deterministic ciphertext differences). **Coverage: 95.4%**
(above the 85% gate).

## Dependencies
Standard library only (`crypto/aes`, `cipher`, `hkdf`, `sha512`, `utf16`).
