'use strict';

const https = require('https');

const ALLOWED_HOST = 'api.weather.gov';

function fetchForecast(zip) {
  return new Promise((resolve, reject) => {
    if (!/^\d{5}$/.test(zip)) {
      reject(new Error('zip must be 5 digits'));
      return;
    }
    const opts = { host: ALLOWED_HOST, path: `/zip/${zip}/forecast`, method: 'GET' };
    const req = https.request(opts, (res) => {
      let body = '';
      res.on('data', (chunk) => (body += chunk));
      res.on('end', () => resolve(JSON.parse(body)));
    });
    req.on('error', reject);
    req.end();
  });
}

module.exports = { fetchForecast };
