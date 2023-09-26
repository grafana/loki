const autoprefixer = require('autoprefixer');
const purgecss = require('@fullhuman/postcss-purgecss');
const whitelister = require('purgecss-whitelister');

module.exports = {
  plugins: [
    autoprefixer(),
    purgecss({
      content: [
        './layouts/**/*.html',
        './content/**/*.md',
        './themes/doks/layouts/**/*.html',
      ],
      safelist: [
        'lazyloaded',
        'table',
        'thead',
        'tbody',
        'tr',
        'th',
        'td',
        ...whitelister([
          './themes/doks/assets/scss/components/_doks.scss',
          './themes/doks/assets/scss/components/_code.scss',
          './themes/doks/assets/scss/components/_search.scss',
          './themes/doks/assets/scss/common/_dark.scss',
          './node_modules/katex/dist/katex.css',
        ]),
      ],
    }),
  ],
}
