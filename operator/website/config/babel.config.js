module.exports = {
  presets: [
    [
      '@babel/preset-env',
      {
        bugfixes: true,
        targets: {
          browsers: ['>= 1%', 'not dead', 'not IE 11', 'not op_mini all'],
        },
      },
    ],
  ],
};
