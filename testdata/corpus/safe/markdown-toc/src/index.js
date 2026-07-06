'use strict';

module.exports = function toc(markdown) {
  if (typeof markdown !== 'string') throw new TypeError('toc(markdown): expected string');
  const lines = markdown.split('\n');
  const out = [];
  for (const line of lines) {
    const m = line.match(/^(#{1,6})\s+(.+)$/);
    if (!m) continue;
    const level = m[1].length;
    const text = m[2].trim();
    const slug = text.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    out.push(`${'  '.repeat(level - 1)}- [${text}](#${slug})`);
  }
  return out.join('\n');
};
