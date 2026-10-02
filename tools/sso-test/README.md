# Self-contained SSO test identities for the OlliTeX TEST stack.
#
# These are the owner-specified reference servers (from
# /data_1/image_mining/benchmark_overleaf_vm/compose/production), copied into
# THIS repo so the test identities are reproducible with NO external-registry
# dependency (unlike the old overleaf-ops/saml-test + rroemhild/test-openldap).
#
#   ldap      osixia/openldap  :389   (service bind cn=ldap_reader / search ou=people)
#   oidc      Flask Keycloak-style IdP :8080  (overleaf_test / SOMEPASSWORD)
#   saml      Flask IdP (metadata + SSOService + cert) :8081
#   mailsink  postfix SMTP capture sink         :25   (or reuse the test-stack mailtrap)
#
# Up everything (reuses local images when present, else builds):
#   docker compose -f tools/sso-test/docker-compose.yml up -d --build
#
# Seed the dedicated LDAP test users (additive, non-destructive):
#   docker compose -f tools/sso-test/docker-compose.yml \
#     run --rm ldap ldapadd -x -H ldap://127.0.0.1:389 \
#       -D 'cn=admin,dc=example,dc=com' -w admin_password -f /seeds/ssoe2e.ldif
#
# Run the Go LIVE LDAP E2E (skipped in CI without a server):
#   LIVE_LDAP_URL=ldap://localhost:389 \
#   LIVE_LDAP_BASE='ou=people,dc=example,dc=com' \
#   LIVE_LDAP_BIND_DN='cn=ldap_reader,dc=example,dc=com' \
#   LIVE_LDAP_BIND_PW=GoodNewsEveryone \
#   go test ./go/services/web/features/sso/ -run TestLdapAuthenticateLive -v
#
# Then point the Go web at them (config surface already implemented in
# go/services/web/features/sso): EXTERNAL_AUTH=ldap and one ssoConfigs doc, e.g.
#   {"type":"ldap","enabled":true,"url":"ldap://ldap:389",
#    "searchBase":"ou=people,dc=example,dc=com","bindDN":"cn=ldap_reader,dc=example,dc=com",
#    "bindCredentials":"GoodNewsEveryone","emailAtt":"mail","firstNameAtt":"givenName",
#    "lastNameAtt":"sn","isAdminAtt":"employeeType","valAdmin":"admin"}
#   or EXTERNAL_AUTH=oidc with the overleaf_test/SOMEPASSWORD client, or EXTERNAL_AUTH=saml
#   with ENTRYPOINT https://<idp-host>/saml/idp/SSOService + the IdP cert.
#
# Seeded LDAP users (see seeds/ssoe2e.ldif):
#   ssoe2e / SsoE2ePass123   ssoe2e@example.com   (regular)
#   ssoadm / SsoAdmPass123   ssoadm@example.com   (employeeType=admin)
# Existing dir users: jdoe, asmith, admin(admin2@example.com,employeeType=admin), bwilson, cjones.
