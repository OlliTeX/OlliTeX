#!/bin/bash
set -ex

# clsi_typst only needs the tools its tests use (pdftotext for output
# verification). Keep this in sync with what test/ needs.
apt-get update
apt-get install -y curl poppler-utils
rm -rf /var/lib/apt/lists/*
