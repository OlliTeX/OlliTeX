{
  "id": "0e2dd3bc",
  "title": "WDV-B (P1): Host static-asset transfer flake — intermittent IncompleteRead/ERR_CONTENT_LENGTH_MISMATCH on large assets (reproduced on OLD image in ol-e2e stack → host-level; likely root of owner's psintern JSON.parse@1413 console crashes). Reproduce under load, localize (ethtool ring/drops, docker-proxy, conntrack, disk I/O), apply/propose fix, re-run e2e editor-console-canary to GREEN as verification gate",
  "tags": [
    "host-flake",
    "e2e-canary",
    "weekend-p1"
  ],
  "status": "completed",
  "created_at": 1790991333831,
  "last_change": 1790992698684,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}


## RESOLVED 2026-10-03 (evidence)
Root cause: **nginx `proxy_buffering` in the OLD overleaf stack (ol-e2e image, nginx 1.30.4 in-container proxying Go-web upstream :4000) truncates static-file responses under concurrent host-client bursts — early FIN mid-body.**
Evidence chain:
- pcap (`Flags [FP.]` after ~179KB of a 283,576B file, no RST) = clean early close by server, repeated at 170–250KB offsets across failing conns.
- Matrix: old build × host-client burst = flaky 40–90%; old build × loopback burst (24/24, ~5ms, curl) = clean; old build × container→container burst (60/60) = clean; NEW build × host client (172.18.0.6 direct + :4000) = 60/60 clean; apache2 control (172.18) = 60/60 clean; upstream Go-web direct (127.0.0.1:4000 in-container, 32×P16) = 32/32 clean → truncation is in the nginx proxy layer, client-path/burst-dependent.
- Fix verified: `proxy_buffering off;` in ol-e2e-overleaf-1 (`/etc/nginx/conf.d/zz-pb-off.conf`) → 60/60 host-client bursts clean.
- Gate: e2e editor-console-canary on :7420 now 2/2 PASS (was failing ERR_CONTENT_LENGTH_MISMATCH).
Explanation of owner's psintern console errors: same mechanism on the old build (JSON.parse SyntaxError @ ~1413 = truncation point of an in-flight chunk); psintern on the new build (7ee6a38385-era image) no longer has the in-container nginx proxy path — host-client bursts to 127.0.0.1:4000/psintern measure 120/120 clean.
Residual: the `zz-pb-off.conf` patch is RUNTIME-ONLY (lost if ol-e2e-overleaf-1 is recreated). Proper fix = re-bake the e2e stack onto the new build (P7/e2e-ownership, owner-gated) OR bake `proxy_buffering off` into the e2e nginx conf. NOT a regression of my work; new build is clean by construction.
