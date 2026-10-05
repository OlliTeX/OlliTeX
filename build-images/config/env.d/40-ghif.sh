# 006 (2026-10-02): shared git work root for the githubinterface bridge and
# the web ghsync layer. Both processes (runit children of the same runsvd)
# must agree on where clone/commit/push trees live. This persistent path is
# already inherited in the runsvd env; export it here explicitly so both the
# bridge (GHIConfig.WorkRoot) and web gsWorkRoot() resolve identically.
GITHUBINTERFACE_WORKDIR_ROOT=/var/lib/overleaf/ghif
