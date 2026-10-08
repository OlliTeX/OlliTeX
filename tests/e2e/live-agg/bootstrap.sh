#!/usr/bin/env bash
# AG e2e reproducible bootstrap (owner directive 2026-10-07):
#  1. Ensure the ag-e2e3@ollitex.local test user exists with a KNOWN password
#     (register -> read one-time token from Mongo -> POST /user/password/set).
#     Reuses the account when it already exists (idempotent).
#  2. Ensure fixture projects 'agg-tex-fixture' (example template:
#     main.tex + sample.bib + frog.jpg) and 'agg-typst-fixture' exist, owned
#     by the test user.
#  3. Log in and verify an authenticated session (GET / -> 302 /hub).
# Uses ONLY public stack APIs + mongosh (token read is the one DB touch,
# because the offline stack cannot deliver the one-time token email).
set -uo pipefail

B="${AGG_BASE:-https://psintern.neuro.uni-bremen.de}"
UA="ag-e2e-bootstrap"
EMAIL="${AGG_EMAIL:-ag-e2e3@ollitex.local}"
PASSWORD="${AGG_PASSWORD:-Agg-E2e-Pass-123}"
JAR="$(mktemp)"
trap 'rm -f "$JAR"' EXIT

csrf_of() { # csrf_of <page.html>
  grep -oE 'name="ol-csrfToken" content="[^"]*"' "$1" 2>/dev/null | head -1 | sed -E 's/.*content="([^"]*)".*/\1/'
}

login() {
  rm -f "$JAR"
  local page
  page="$(mktemp)"
  curl -sk -c "$JAR" -A "$UA" "$B/login" -o "$page"
  local cs; cs="$(csrf_of "$page")"; rm -f "$page"
  curl -sk -b "$JAR" -c "$JAR" -A "$UA" -X POST "$B/login" \
    --data-urlencode "email=$EMAIL" \
    --data-urlencode "password=$PASSWORD" \
    --data-urlencode "_csrf=$cs" -o /dev/null -w '%{http_code}'
}

echo "== 1. ensure test user $EMAIL =="
code="$(login)"
if [ "$code" = "302" ]; then
  echo "   already logged in (302)"
else
  echo "   login failed ($code) -> (re)enabling via registration token"
  rm -f "$JAR"
  page="$(mktemp)"
  curl -sk -c "$JAR" -A "$UA" "$B/register" -o "$page"
  cs="$(csrf_of "$page")"; rm -f "$page"
  rcode="$(curl -sk -b "$JAR" -c "$JAR" -A "$UA" -X POST "$B/register" \
    -H 'content-type: application/json' -H "x-csrf-token: $cs" \
    -d "{\"first_name\":\"Agg\",\"last_name\":\"E3\",\"email\":\"$EMAIL\"}" -o /dev/null -w '%{http_code'})"
  echo "   register -> $rcode (409 taken is fine; 422 mail-fail is expected offline)"
  TOK="$(docker exec ollitex-mongo mongosh mongodb://172.30.0.1:27017/ollitex --quiet \
    --eval "const a=db.tokens.find({data:{email:'$EMAIL'}}).toArray(); if(a.length) print(a[a.length-1].token);" 2>/dev/null \
    | grep -oE '[0-9a-f]{64}' | head -1)"
  if [ -z "$TOK" ]; then
    echo "   ERROR: no one-time token found for $EMAIL (account state broken)" >&2
    exit 1
  fi
  sjar="$(mktemp)"
  page="$(mktemp)"
  curl -sk -c "$sjar" -A "$UA" "$B/login" -o "$page"
  scs="$(csrf_of "$page")"; rm -f "$page"
  setcode="$(curl -sk -b "$sjar" -c "$sjar" -A "$UA" -X POST "$B/user/password/set" \
    --data-urlencode "email=$EMAIL" \
    --data-urlencode "password=$PASSWORD" \
    --data-urlencode "passwordResetToken=$TOK" \
    --data-urlencode "_csrf=$scs" -o /dev/null -w '%{http_code'})"
  rm -f "$sjar"
  echo "   password/set -> $setcode"
  code="$(login)"
  [ "$code" = "302" ] || { echo "   ERROR: still cannot login ($code)" >&2; exit 1; }
  echo "   login OK (302)"
fi

echo "== 2. ensure fixture projects =="
for spec in "agg-tex-fixture|/project/new|{\"projectName\":\"agg-tex-fixture\",\"template\":\"example\"}" \
            "agg-typst-fixture|/project/new/typst|{\"projectName\":\"agg-typst-fixture\"}"; do
  title="${spec%%|*}"; rest="${spec#*|}"; path="${rest%%|*}"; body="${rest#*|}"
  have="$(docker exec ollitex-mongo mongosh mongodb://172.30.0.1:27017/ollitex --quiet \
    --eval "const p=db.projects.findOne({name:'$title'}); if(p) print(p._id.toString());" 2>/dev/null \
    | grep -oE '[0-9a-f]{24}' | head -1)"
  if [ -n "$have" ]; then
    echo "   $title: exists ($have)"
    continue
  fi
  page="$(mktemp)"
  curl -sk -b "$JAR" -c "$JAR" -A "$UA" -L "$B/" -o "$page"
  fcs="$(csrf_of "$page")"; rm -f "$page"
  ccode="$(curl -sk -b "$JAR" -c "$JAR" -A "$UA" -X POST "$B$path" \
    -H 'content-type: application/json' -H "x-csrf-token: $fcs" \
    -d "$body" -o /var/tmp/aggsig_out.json -w '%{http_code'})"
  echo "   $title: created ($ccode) $(head -c 120 /var/tmp/aggsig_out.json 2>/dev/null)"
done

echo "== 3. authenticated smoke check =="
code="$(curl -sk -b "$JAR" -A "$UA" "$B/" -o /dev/null -w '%{http_code}')"
loc="$(curl -sk -b "$JAR" -A "$UA" "$B/" -o /dev/null -D - | grep -i '^location' | head -1)"
echo "   GET / -> $code ($loc)"
[ "$code" = "302" ] || [ "$code" = "200" ] || { echo "   ERROR: not authenticated" >&2; exit 1; }
echo "== bootstrap OK =="
