'use strict';

const COLORS = [31, 33, 32, 34, 35]; // red, yellow, green, blue, magenta (ANSI)

module.exports = function format(code) {
  if (typeof code !== 'string') {
    throw new TypeError('format(code): expected string');
  }
  let out = '';
  let i = 0;
  for (const ch of code) {
    out += `[${COLORS[i % COLORS.length]}m${ch}`;
    i++;
  }
  return out + '[0m';
};
