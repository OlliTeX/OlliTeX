// 2026-09 (owner #10): sandboxed (Docker) compiles are MANDATORY in OlliTeX —
// the local (in-container) compile runner was removed. See
// config/settings.defaults.cjs (clsi.dockerRunner) and DockerRunner.mjs.
import Settings from '@overleaf/settings'
import logger from '@overleaf/logger'

const commandRunnerPath = './DockerRunner.mjs'
logger.debug({ commandRunnerPath }, 'selecting command runner for clsi (mandatory: sandboxed)')

if ((Settings.clsi != null ? Settings.clsi.dockerRunner : undefined) !== true) {
  // Defensive: the config in this repository always enables the docker runner.
  // Fail loudly rather than silently running compiles without a sandbox.
  console.error('clsi requires sandboxed compiles (clsi.dockerRunner=true). This is enforced for OlliTeX; refusing to start with a local runner.')
  process.exit(1)
}

const CommandRunner = (await import(commandRunnerPath)).default

export default CommandRunner
