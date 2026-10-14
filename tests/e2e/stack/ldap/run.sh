#!/bin/sh
mkdir -p /tmp/ld/data
slapd -f /tmp/ld/slapd.conf -h "ldap://0.0.0.0:3890" -d0
