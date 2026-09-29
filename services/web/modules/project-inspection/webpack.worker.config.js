// Webpack build for the project-inspection analysis worker bundle.
// Run from services/web:
//   yarn exec webpack --config modules/project-inspection/webpack.worker.config.js
// Produces modules/project-inspection/dist/analyze-worker.cjs — a
// self-contained CJS bundle (engine + lezer parsers inlined; only Node
// builtins external) that the Go web runs as a short-lived process:
//   node dist/analyze-worker.cjs   (stdin snapshot JSON -> stdout result)
// The same file is imported by the unit tests (dist is the tested
// artifact; vitest's CJS interop exposes the re-exported names).

const Path = require('path')

module.exports = {
  mode: 'none',
  target: 'node',
  entry: Path.join(__dirname, 'worker-entry.mjs'),
  output: {
    path: Path.join(__dirname, 'dist'),
    filename: 'analyze-worker.cjs',
    library: {
      type: 'commonjs2'
    }
  },
  externalsPresets: {
    node: true
  },
  resolve: {
    extensions: ['.mjs', '.js'],
    fullySpecified: false
  },
  optimization: {
    minimize: false,
    moduleIds: 'named',
    chunkIds: 'named'
  },
  stats: 'errors-warnings'
}
