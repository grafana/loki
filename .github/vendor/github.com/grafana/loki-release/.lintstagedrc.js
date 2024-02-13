const PRETTIER_WRITE = 'prettier --write';
const JSONNETFMT = 'jsonnetfmt -i -n 2';

module.exports = {
  '**/!(package).json': [PRETTIER_WRITE],
  'actions/*/src/**/*.{js,jsx,ts,tsx}': ['eslint --fix', PRETTIER_WRITE],
  'workflows/**/*.{jsonnet,libsonnet}': [JSONNETFMT],
};
