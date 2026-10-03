const { merge } = require('webpack-merge')
const TerserPlugin = require('terser-webpack-plugin')
const CssMinimizerPlugin = require('css-minimizer-webpack-plugin')
const MiniCssExtractPlugin = require('mini-css-extract-plugin')

const base = require('./webpack.config')

module.exports = merge(
  base,
  {
    mode: 'production',

    // 2026-09 (IMPROVEMENTS P0.5): the two typst wasm modules (typstyle,
    // codemirror-lang-typst) require an environment with async/await. The base
    // config inherited webpack's conservative 'web' defaults, which flagged
    // them with "target environment does not appear to support async/await".
    // Our supported browsers (browserslist: defaults+woff2, last 1 year,
    // safari >= 15) all ship es2020+. es2017 already enables async/await;
    // es2020 is the safe ceiling for optional chaining used elsewhere too.
    target: ['web', 'es2020'],

    // 2026-09 (IMPROVEMENTS P0.5): silence the asset/entrypoint size warnings
    // intentionally — local fonts (Noto Serif/STIX ~1 MB) and the 762 MB
    // single-bundle hub are documented, known debt (the bundle-diet item is
    // P1 #11); the warnings added no signal in the green-gate logs.
    performance: { hints: false },


    // Enable a full source map. Generates a comment linking to the source map
    devtool: 'hidden-source-map',

    optimization: {
      // Minify JS (with Terser) and CSS (with cssnano)
      minimizer: [
        new TerserPlugin({
          terserOptions: {
            keep_classnames: /(Error|Exception)$/,
            keep_fnames: /(Error|Exception)$/,
          },
        }),
        new CssMinimizerPlugin({
          minimizerOptions: {
            // disable mergeLonghand to avoid a cssnano bug https://github.com/cssnano/cssnano/issues/864
            preset: ['default', { mergeLonghand: false }],
          },
        }),
      ],
    },

    plugins: [
      // Extract CSS to a separate file (rather than inlining to a <style> tag)
      new MiniCssExtractPlugin({
        // Output to public/stylesheets directory and append hash for immutable
        // caching
        filename: 'stylesheets/[name]-[contenthash].css',
      }),
    ],
  },
  {}
)
