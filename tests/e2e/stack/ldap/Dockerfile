# e2e OpenLDAP world asset (SSO leg-b → CI matrix, owner-approved 2026-10-14).
#
# Faithful reconstruction of the proven olpsint/e2e-ldaptest:v1 (7/7 x2 on
# sso-ldap-live): Debian bookworm-slim + slapd 2.5.13 (back_mdb, old-style
# slapd.conf at /tmp/ld, seeded dc=e2e,dc=test dir). Compose builds the image
# on first stack:up (fresh CI runners) and keeps it tagged for repeat runs.
#
# Fixtures (e2e-only, disposable world — same convention as the spec's own
# constants): admin rootdn cn=admin,dc=e2e,dc=test / Ol-LdapRoot-9x7K
# user uid=e2e-ldap,ou=users,dc=e2e,dc=test (mail e2e-ldap@e2e.test) /
# Ol-LdapUser-3m2Q
FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends slapd ldap-utils \
 && rm -rf /var/lib/apt/lists/*

COPY slapd.conf /tmp/ld/slapd.conf
COPY init.ldif  /tmp/ld/init.ldif

# Seed the mdb at build time (slapadd loads back_mdb via the conf's
# modulepath/moduleload), then drop the seed so the runtime image carries
# only the live directory.
RUN mkdir -p /tmp/ld/data \
 && chown -R openldap:openldap /tmp/ld/data \
 && slapadd -f /tmp/ld/slapd.conf -l /tmp/ld/init.ldif \
 && rm /tmp/ld/init.ldif

COPY run.sh /tmp/ld/run.sh
RUN chmod +x /tmp/ld/run.sh

EXPOSE 3890
# ready = anonymous base query answers (slapd bound on 3890)
HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=30 \
  CMD ldapsearch -x -H ldap://127.0.0.1:3890 -s base > /dev/null 2>&1 || exit 1

CMD ["/tmp/ld/run.sh"]
